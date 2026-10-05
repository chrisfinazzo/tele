package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/gotd/td/telegram/updates"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/config"
	"github.com/sorokin-vladimir/tele/internal/core"
	"github.com/sorokin-vladimir/tele/internal/core/outbox"
	"github.com/sorokin-vladimir/tele/internal/core/state"
	"github.com/sorokin-vladimir/tele/internal/store"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
)

// account is everything tele holds for one account while it runs: the
// database, the connection, the owner over them, the send queue and the
// caches. It is built whole and stopped whole. The host outlives it and holds
// nothing of it, which is what lets a log out end one account and a login start
// the next in the same process (#297).
type account struct {
	store *store.SQLiteStore
	owner *core.Owner
	log   *zap.Logger

	cancel context.CancelFunc
	// loops counts the connection and the two owner loops started by run.
	loops    sync.WaitGroup
	stopOnce sync.Once
}

// accountDeps is what the host hands an account it opens: its own things, which
// last longer than any account, and how to reach Telegram.
type accountDeps struct {
	cfg      *config.Config
	log      *zap.Logger
	tmpDir   string
	notifier core.Notifier
	// connect builds the connection over the account's update state storage.
	connect func(updates.StateStorage) core.Connection
	// onAuth is told who logged in, once the account knows.
	onAuth func(userID int64, username string)
}

// openAccount builds an account over the state directory: the database, the
// connection, the owner, the send queue and the caches. Nothing runs yet.
func openAccount(d accountDeps) (*account, error) {
	statePath := filepath.Join(d.cfg.StateDir, "state.db")
	sqliteStore, err := store.NewSQLite(statePath, d.log)
	if err != nil {
		return nil, fmt.Errorf("open state DB: %w", err)
	}
	conn := d.connect(internaltg.NewSQLiteStateStorage(sqliteStore.DB()))
	owner := core.New(d.cfg, d.log, state.New(sqliteStore), conn, d.notifier)
	// The client finds a skewed clock in what gotd logs; the owner hands it on
	// to whoever is drawing (#277).
	if skewed, ok := conn.(interface{ SetOnClockSkew(func(time.Duration)) }); ok {
		skewed.SetOnClockSkew(owner.SetClockSkew)
	}

	// The send queue shares the account database: the file DB runs on a single
	// connection (#119), and a second one to the same file is how SQLITE_BUSY
	// came back last time.
	sendQueue, err := outbox.NewStore(sqliteStore.DB())
	if err != nil {
		_ = sqliteStore.Close()
		return nil, fmt.Errorf("open outbox: %w", err)
	}
	owner.SetOutbox(sendQueue)

	if cache, cerr := openMediaCache(d.cfg, d.tmpDir, d.log); cerr != nil {
		d.log.Warn("media cache unavailable; media will not be cached", zap.Error(cerr))
	} else {
		owner.SetMediaCache(cache)
	}
	if cache, cerr := openAvatarCache(d.cfg, d.tmpDir, d.log); cerr != nil {
		d.log.Warn("avatar cache unavailable; avatars will not be shown", zap.Error(cerr))
	} else {
		owner.SetAvatarCache(cache)
	}
	if d.onAuth != nil {
		owner.SetOnAuth(d.onAuth)
	}
	return &account{store: sqliteStore, owner: owner, log: d.log}, nil
}

// run connects and starts the owner's two loops, all under a context of the
// account's own that ends with stop or with parent. onEnd is told what the
// connection ended with; it is called once, from the connection's goroutine.
func (a *account) run(parent context.Context, onEnd func(error)) {
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	a.loops.Add(3)
	go func() {
		defer a.loops.Done()
		err := a.owner.Start(ctx)
		if onEnd != nil {
			onEnd(err)
		}
	}()
	go func() {
		defer a.loops.Done()
		a.owner.RunUpdates(ctx)
	}()
	go func() {
		defer a.loops.Done()
		a.owner.RunOutbox(ctx)
	}()
}

// stop ends the account and waits for it: the connection and the loops, then
// the owner's own background work, then the database. Once it returns nothing
// of the account is running and its files may be removed. Safe to call more
// than once, and on an account that never ran.
func (a *account) stop() {
	a.stopOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		a.loops.Wait()
		a.owner.Stop()
		if err := a.store.Close(); err != nil {
			a.log.Warn("closing the account database", zap.Error(err))
		}
	})
}
