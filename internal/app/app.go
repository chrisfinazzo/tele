package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/gotd/td/telegram/updates"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/appkey"
	"github.com/sorokin-vladimir/tele/internal/config"
	"github.com/sorokin-vladimir/tele/internal/core"
	"github.com/sorokin-vladimir/tele/internal/notices"
	"github.com/sorokin-vladimir/tele/internal/proxy"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
	"github.com/sorokin-vladimir/tele/internal/ui"
	"github.com/sorokin-vladimir/tele/internal/ui/keys"
)

type App struct {
	// cfgStore is the config file and the config the app is running on. The app
	// asks it rather than holding a Config of its own, so that a reload reaches
	// everything through one place.
	cfgStore *config.Store
	log      *zap.Logger
	// startup is what the process settled when it started, handed to every
	// account it opens in place of what the file says by then (#239).
	startup startup

	// The App is the host: it outlives the accounts that pass through the
	// process and holds nothing of theirs (#297). acct is the one running now
	// and epoch the turn it has on screen; both change together, under mu,
	// when an account ends and the next one starts.
	mu    sync.Mutex
	acct  *account
	epoch uint64
	// switching serialises the end of one account and the start of the next
	// with the stop of whichever is running when the process exits.
	switching sync.Mutex
	// exitErr is what the last account's connection ended with, for the exit
	// code.
	exitErr error

	// What the host needs to open each account: how to connect to Telegram and
	// where desktop notifications go.
	connect  func(internaltg.Endpoint, updates.StateStorage) core.Connection
	notifier core.Notifier

	// prog draws whichever account is running, and deliver hands it a message;
	// keyMap and noticeSeen are the person's and the machine's, so every
	// account's model shares them.
	prog       *tea.Program
	deliver    func(tea.Msg)
	keyMap     keys.KeyMap
	noticeSeen notices.Seen

	// tmpDir holds this run's scratch files: media saved for an external
	// player, GIFs staged for decoding, and the media cache when the user asked
	// for no persistent one. Each account works in a directory of its own
	// inside it, removed when the account ends. Removed on exit.
	tmpDir  string
	verbose bool
	// stateMoved reports that startup migration relocated the account state, so
	// the user can be told where it went.
	stateMoved bool
	// logPath is this run's log file, known only to the caller that opened it.
	logPath string
	// selfMu guards the account identity: it arrives on the Telegram goroutine
	// and is read on exit, after the TUI is gone.
	selfMu       sync.Mutex
	selfID       int64
	selfUsername string
}

// SetStateMoved records whether startup migration relocated the account state.
func (a *App) SetStateMoved(moved bool) { a.stateMoved = moved }

// SetLogPath records where this run's log is written, so the farewell banner
// can point at it.
func (a *App) SetLogPath(path string) { a.logPath = path }

func (a *App) setSelf(userID int64, username string) {
	a.selfMu.Lock()
	defer a.selfMu.Unlock()
	a.selfID, a.selfUsername = userID, username
}

func (a *App) self() (int64, string) {
	a.selfMu.Lock()
	defer a.selfMu.Unlock()
	return a.selfID, a.selfUsername
}

// pendingNotices lists the one-time startup messages this build can show.
// Conditional entries are omitted when they do not apply, so a user only ever
// sees what actually happened on their machine.
func (a *App) pendingNotices() []notices.Notice {
	const delay = 7 * time.Second
	out := []notices.Notice{
		{
			ID:    "single-instance-v1.10",
			Title: "Only one tele at a time",
			Delay: delay,
			Body: "Starting a second tele on the same account now fails with a message " +
				"instead of starting. Two instances shared one session and one database " +
				"with nothing arbitrating between them, quietly overwriting each other's " +
				"unread counts and sync state, which surfaced later as missed messages.",
		},
	}
	if a.stateMoved {
		out = append(out, notices.Notice{
			ID:    "state-dir-moved-v1.10",
			Title: "Your data moved",
			Delay: delay,
			Body: "The session and local database now live in " + a.startup.stateDir +
				", instead of next to the config file. They were moved for you and " +
				"nothing was lost: you are still logged in. The old location is now empty " +
				"and can be ignored.",
		})
	}
	if a.startup.sessionPinned {
		out = append(out, notices.Notice{
			ID:    "session-file-deprecated-v1.10",
			Title: "session_file is going away",
			Delay: delay,
			Body: "Your config sets telegram.session_file, so your session was left exactly " +
				"where it is. That setting is deprecated and will be removed in the next " +
				"release. Replace it with state_dir pointing at the directory that should " +
				"hold the session and the database.",
		})
	}
	return out
}

// cfg is the config the app is running on. Asked for rather than held, so that
// a reload is seen by whoever asks next.
func (a *App) cfg() *config.Config { return a.cfgStore.Current() }

// reloadConfig re-reads the config file and hands the result to everything that
// holds one, before returning it to the UI to apply to itself. This is the whole
// of "apply what is on disk": one function, called whether the change was made
// in the settings overlay or in an editor (ADR 0009).
func (a *App) reloadConfig() (*config.Config, error) {
	if err := a.cfgStore.Reload(); err != nil {
		return nil, err
	}
	cfg := a.cfg()
	acct, _ := a.current()
	acct.owner.SetConfig(cfg)
	return cfg, nil
}

// checkRoute settles how this process will reach Telegram, before anything is
// drawn. Both things it does belong before the interface: the route is named in
// the log, and a declared proxy is dialled once so a server nobody is listening
// on is a message on the terminal rather than an endless reconnect.
func checkRoute(route proxy.Route, path string, log *zap.Logger) error {
	// One line, at Info so it shows without -e. It names the address, because a
	// wrong port has to be visible in a log somebody pastes into an issue, and
	// never the secret or the password.
	log.Info("telegram route", zap.String("proxy", route.Describe()))

	if err := proxy.Probe(context.Background(), route); err != nil {
		return fmt.Errorf("%w (set proxy.type: direct in %s to connect without a proxy)", err, path)
	}
	return nil
}

// New builds the host over the config the process started with and the app key
// it resolved. The key is handed in rather than read from the config: it is
// settled from the file and from how the binary was built, and the file alone
// does not know the second (#239).
func New(cfgStore *config.Store, key appkey.Key, log *zap.Logger, verbose bool, trace bool) (*App, error) {
	start, err := newStartup(cfgStore.Current(), key)
	if err != nil {
		return nil, err
	}
	if err := checkRoute(start.route, cfgStore.Path(), log); err != nil {
		return nil, err
	}

	// The temp directory is created here rather than in Run because the media
	// cache may live inside it, and the owner needs the cache before it starts.
	// It is the host's: it outlives the accounts that cache into it.
	tmpDir, err := os.MkdirTemp("", "tele-*")
	if err != nil {
		log.Warn("failed to create temp dir for media", zap.Error(err))
		tmpDir = ""
	}
	removeLegacyMediaCache(log)

	a := &App{
		cfgStore: cfgStore,
		log:      log,
		startup:  start,
		connect: func(endpoint internaltg.Endpoint, stateStorage updates.StateStorage) core.Connection {
			return internaltg.NewGotdClient(log, endpoint, stateStorage, trace)
		},
		notifier: newNotifier(log),
		tmpDir:   tmpDir,
		verbose:  verbose,
	}
	if _, _, err := a.openNext(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) Run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Removed last: the account stopping below may still have caches in it.
	defer os.RemoveAll(a.tmpDir) //nolint:errcheck

	km, warns := keys.MergeOverrides(keys.DefaultKeyMap(), a.cfg().KeybindingOverrides())
	for _, w := range warns {
		a.log.Warn("keybindings: " + w)
	}
	a.keyMap = km
	acct, epoch := a.current()
	a.noticeSeen = a.openNoticeSeen(acct)

	a.prog = tea.NewProgram(ui.NewShell(epoch, a.buildRoot(acct, true)))
	a.deliver = a.prog.Send
	a.launch(ctx, acct, epoch)

	_, err := a.prog.Run()
	cancel()
	// Whichever account is running when the program ends stops here, and not
	// while another is taking its place.
	a.switching.Lock()
	last, _ := a.current()
	last.stop()
	a.switching.Unlock()

	// Disable OS color-scheme reports (DEC mode 2031) enabled at startup, so the
	// terminal stops emitting report sequences to the shell after tele exits
	// (issue #148). The program has restored the normal screen by now, so write
	// the reset directly.
	_, _ = fmt.Fprint(os.Stdout, ansi.ResetModeLightDark)

	// Leave something in the scrollback: which build ran, as whom, and where to
	// look afterwards. Skipped when the run failed (the error is the message) or
	// when stdout is not a terminal, so piping tele stays clean.
	if err == nil && term.IsTerminal(os.Stdout.Fd()) {
		home, _ := os.UserHomeDir()
		_, _ = fmt.Fprint(os.Stdout, a.farewell(home))
	}

	if exitErr := a.lastExitErr(); exitErr != nil && err == nil {
		return fmt.Errorf("telegram: %w", exitErr)
	}
	return err
}
