package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	tea "charm.land/bubbletea/v2"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/accountstate"
	"github.com/sorokin-vladimir/tele/internal/core"
	"github.com/sorokin-vladimir/tele/internal/notices"
	"github.com/sorokin-vladimir/tele/internal/ui"
	"github.com/sorokin-vladimir/tele/internal/ui/components"
	"github.com/sorokin-vladimir/tele/internal/ui/screens"
)

// The host's side of an account's life (#297). The host opens an account,
// builds the model that draws it and runs both; when the account ends with a
// log out, the host stops it whole, removes it from disk, opens the next one
// and hands the client that one's model. Nothing of an account lives in the
// host, which is what lets the next one start with nothing of the last.

// current is the account running now and the epoch it has on screen.
func (a *App) current() (*account, uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.acct, a.epoch
}

// openNext opens the next account over the state directory and makes it the
// current one, with the next epoch. Its scratch files live in a directory of
// its own, so they go with it rather than pass to whoever logs in next.
func (a *App) openNext() (*account, uint64, error) {
	a.mu.Lock()
	epoch := a.epoch + 1
	a.mu.Unlock()

	cfg := a.cfg()
	tmpDir := ""
	if a.tmpDir != "" {
		tmpDir = filepath.Join(a.tmpDir, "account-"+strconv.FormatUint(epoch, 10))
		if err := os.MkdirAll(tmpDir, 0o700); err != nil {
			a.log.Warn("no scratch directory for the account", zap.Error(err))
			tmpDir = ""
		}
	}
	acct, err := openAccount(accountDeps{
		cfg:      cfg,
		startup:  a.startup,
		log:      a.log,
		tmpDir:   tmpDir,
		notifier: a.notifier,
		connect:  a.connect,
		// The account identity is needed both by the message list (own
		// messages) and by the farewell banner on exit.
		onAuth: func(userID int64, username string) {
			if err := accountstate.Record(a.startup.stateDir, a.startup.sessionFile); err != nil {
				a.log.Error("record account identity", zap.Error(err))
			}
			components.SetSelfIdentity(userID, username)
			a.setSelf(userID, username)
		},
	})
	if err != nil {
		return nil, 0, err
	}

	a.mu.Lock()
	a.acct, a.epoch = acct, epoch
	a.mu.Unlock()
	return acct, epoch, nil
}

// buildRoot builds the model that draws acct. The person's things - config,
// key map, log path - are the host's and shared; everything the model reaches
// the account through is this account's. The one-time startup notices belong
// to the start of the process, so only the first account's model has them.
func (a *App) buildRoot(acct *account, first bool) ui.RootModel {
	cfg := a.cfg()
	root := ui.NewRootModel(cfg.UI.HistoryLimit, a.verbose)
	// The client attaches: the focus it reports belongs to it, everything else
	// is the owner's (#192). It ends with the owner.
	root = root.WithContext(acct.ctx).WithImageMode(acct.startup.imageMode).WithConfig(cfg).WithKeyMap(a.keyMap).WithOwner(acct.owner.Attach()).
		WithLogger(a.log).WithConfigReload(a.reloadConfig).WithSettingsStore(a.cfgStore).WithLogPath(a.logPath)
	root.SetLoginModel(screens.NewLoginModel(acct.owner.AuthFlow()))
	root.SetTmpDir(acct.tmpDir)
	if first {
		// Seen-state is written on dismissal, so quitting before the countdown
		// ends shows the notice again next time (#197).
		root = root.WithNotices(notices.Pending(a.pendingNotices(), a.noticeSeen), a.noticeSeen)
		// The themes loaded at startup may have been reloaded since, and a
		// reload says what is wrong with them itself.
		root = root.WithThemeWarnings(a.themeWarnings)
	}
	return root
}

// openNoticeSeen opens the machine's record of the notices already shown. An
// earlier build kept it in the account's database, which a log out removes;
// what that database says is carried over once, the first time the file is
// made, so an upgrade shows nothing twice.
func (a *App) openNoticeSeen(acct *account) notices.Seen {
	seen, existed := notices.NewFileSeen(filepath.Join(a.startup.stateDir, "notices.json"))
	if !existed {
		var ids []string
		for _, n := range a.pendingNotices() {
			ids = append(ids, n.ID)
		}
		notices.CarryOver(seen, notices.NewSQLiteSeen(acct.store.DB()), ids)
	}
	return seen
}

// send delivers msg to the model of epoch, and to no other: a late message
// from an account that ended is dropped on arrival.
func (a *App) send(epoch uint64, msg tea.Msg) {
	a.deliver(ui.Stamped(epoch, msg))
}

// launch runs acct with the work the host does for it: the two bridges to its
// model, and listening for the client to ask for the account to end.
func (a *App) launch(ctx context.Context, acct *account, epoch uint64) {
	acct.run(ctx, a.connectionEnded(ctx, acct, epoch),
		a.authBridge(acct, epoch), a.deltaBridge(acct, epoch), a.endRequests(ctx, acct, epoch))
}

// endRequests ends acct when its client asks: for an end the connection did not
// bring about itself, such as leaving a banned account (#297).
func (a *App) endRequests(hostCtx context.Context, acct *account, epoch uint64) func(context.Context) {
	return func(ctx context.Context) {
		select {
		case <-ctx.Done():
		case reason := <-acct.owner.EndRequests():
			// On a goroutine of its own: ending the account waits for this
			// work, which is the account's.
			go a.endAccount(hostCtx, acct, epoch, reason)
		}
	}
}

// connectionEnded is told what acct's connection ended with. A log out ends the
// account and starts the next one. Anything else is kept for the exit code and
// shown while the program still runs: waiting for the exit to report it meant
// nobody saw it, on a screen nobody could quit (#283).
func (a *App) connectionEnded(ctx context.Context, acct *account, epoch uint64) func(error) {
	return func(err error) {
		if ctx.Err() == nil {
			if reason, ok := core.EndReasonOf(err); ok {
				// On a goroutine of its own: ending the account waits for this
				// one, which is still the connection's.
				go a.endAccount(ctx, acct, epoch, reason)
				return
			}
		}
		a.mu.Lock()
		a.exitErr = err
		a.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			a.send(epoch, ui.ConnectFailedMsg{Err: err})
		}
	}
}

func (a *App) lastExitErr() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitErr
}

// endAccount ends acct for reason and starts the next account in its place:
// it stops acct whole, removes it from disk with its session, opens the next
// one and puts that one's login on screen with the reason it is there (#297,
// ADR 0023 in the notes). If the removal fails, the next account is not
// opened: starting over the last one's files is what this exists to prevent.
func (a *App) endAccount(ctx context.Context, acct *account, epoch uint64, reason core.EndReason) {
	a.switching.Lock()
	defer a.switching.Unlock()
	if ctx.Err() != nil {
		return // the process is ending; its exit stops the account
	}
	discarded := acct.owner.UnsentCount()
	acct.stop()

	if err := accountstate.End(acct.startup.stateDir, acct.startup.sessionFile); err != nil {
		a.log.Error("the account that ended could not be removed", zap.Error(err))
		a.send(epoch, ui.ConnectFailedMsg{Err: fmt.Errorf("the account that ended could not be removed: %w", err)})
		return
	}
	if acct.tmpDir != "" {
		_ = os.RemoveAll(acct.tmpDir)
	}
	a.log.Info("account ended", zap.String("reason", string(reason)), zap.Int("discarded", discarded))
	// What the ended account's connection ended with is not the process's
	// to exit on.
	a.mu.Lock()
	a.exitErr = nil
	a.mu.Unlock()

	next, nextEpoch, err := a.openNext()
	if err != nil {
		a.log.Error("the next account could not be opened", zap.Error(err))
		a.send(epoch, ui.ConnectFailedMsg{Err: err})
		return
	}
	a.deliver(ui.AccountEndedMsg{
		Epoch: nextEpoch,
		Root:  a.buildRoot(next, false),
		Ended: core.AccountEnded{Reason: reason, Discarded: discarded},
	})
	a.launch(ctx, next, nextEpoch)
}

// authBridge carries acct's login to its model, and once logged in loads the
// dialog list and the folders. It is the account's work: stop waits for it,
// since loading writes to the database stop closes.
func (a *App) authBridge(acct *account, epoch uint64) func(context.Context) {
	return func(ctx context.Context) {
		owner := acct.owner
		for {
			msg := screens.WaitForAuthRequest(ctx, owner.AuthFlow(), owner.Ready())()
			if msg == nil {
				return
			}
			a.send(epoch, msg)
			if req, isReq := msg.(screens.AuthRequestMsg); isReq {
				a.log.Debug("auth step requested", zap.Int("step", int(req.Step)))
			}
			if _, done := msg.(screens.ConnectedMsg); done {
				a.log.Info("connected, loading dialogs")
				break
			}
			if errMsg, failed := msg.(screens.AuthErrorMsg); failed {
				a.log.Error("auth error", zap.String("reason", errMsg.Text))
				return
			}
		}

		var loads sync.WaitGroup
		loads.Add(2)
		// Connected: the owner loads the authoritative dialog list.
		go func() {
			defer loads.Done()
			if err := owner.Bootstrap(ctx); err != nil {
				a.log.Error("GetDialogs failed", zap.Error(err))
				return
			}
			a.send(epoch, screens.TransitionToMainMsg{})
		}()
		// Cached folder filters at once, then the network's.
		if cached := acct.store.FolderFilters(); len(cached) > 0 {
			a.send(epoch, ui.FolderFiltersMsg{Filters: cached})
		}
		go func() {
			defer loads.Done()
			filters, err := owner.LoadFolderFilters(ctx)
			if err != nil {
				a.log.Warn("GetDialogFilters failed", zap.Error(err))
				return
			}
			if len(filters) > 0 {
				a.send(epoch, ui.FolderFiltersMsg{Filters: filters})
			}
		}()
		loads.Wait()
	}
}

// deltaBridge carries what acct's owner publishes to its model.
func (a *App) deltaBridge(acct *account, epoch uint64) func(context.Context) {
	return func(ctx context.Context) {
		owner := acct.owner
		for {
			var msg tea.Msg
			select {
			case <-ctx.Done():
				return
			case msg = <-owner.Deltas():
			case msg = <-owner.Incoming():
			case msg = <-owner.Notifications():
			case msg = <-owner.Failures():
			case msg = <-owner.Typing():
			case msg = <-owner.Progress():
			case msg = <-owner.Downloads():
			case msg = <-owner.ClockSkew():
			}
			a.send(epoch, msg)
		}
	}
}
