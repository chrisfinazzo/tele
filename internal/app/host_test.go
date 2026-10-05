package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/telegram/updates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/appkey"
	"github.com/sorokin-vladimir/tele/internal/config"
	"github.com/sorokin-vladimir/tele/internal/core"
	"github.com/sorokin-vladimir/tele/internal/store"
	"github.com/sorokin-vladimir/tele/internal/telerr"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
	"github.com/sorokin-vladimir/tele/internal/ui"
)

// loggedOutConnection connects only to learn the session was logged out
// elsewhere, which is how a connection ends when the account does.
type loggedOutConnection struct {
	internaltg.Client
	updates chan store.Event
}

func (c *loggedOutConnection) Connect(context.Context, *internaltg.AuthFlow,
	chan<- struct{}, func(int64, string)) error {
	return &telerr.Error{Kind: telerr.Unauthorized, LogOut: telerr.LogOutElsewhere}
}

func (c *loggedOutConnection) Updates() <-chan store.Event { return c.updates }

// bannedConnection connects only to learn the account is banned: the
// connection ends, and the account stays until the person leaves it.
type bannedConnection struct {
	internaltg.Client
	updates chan store.Event
}

func (c *bannedConnection) Connect(context.Context, *internaltg.AuthFlow,
	chan<- struct{}, func(int64, string)) error {
	return &telerr.Error{Kind: telerr.AccountBanned}
}

func (c *bannedConnection) Updates() <-chan store.Event { return c.updates }

// testHost is a host over a fresh state directory whose first account is
// logged out as soon as it connects, and whose next one stays connected.
func testHost(t *testing.T) (*App, chan tea.Msg, string) {
	t.Helper()
	return testHostWith(t, &loggedOutConnection{updates: make(chan store.Event)})
}

// testHostWith is testHost with first as the first account's connection.
func testHostWith(t *testing.T, first core.Connection) (*App, chan tea.Msg, string) {
	t.Helper()
	h := newTestHost(t, first, appkey.Key{}, "ui:\n  history_limit: 30\n")
	return h.app, h.delivered, h.app.startup.sessionFile
}

// testHostRig is a host over a real config file and a fresh state directory,
// with what its accounts were handed.
type testHostRig struct {
	app       *App
	delivered chan tea.Msg
	// endpoints receives what each account's connection was made with.
	endpoints chan internaltg.Endpoint
	// path is the config file, and stateDir the platform state directory.
	path     string
	stateDir string
}

// newTestHost builds a host the way New does, over a config file that says
// body, with key as the app key the process resolved. Its first account's
// connection is first; every later one stays connected.
func newTestHost(t *testing.T, first core.Connection, key appkey.Key, body string) testHostRig {
	t.Helper()
	stateDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	cfg, err := config.Load(path, stateDir)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cfg.StateDir, 0o700))
	require.NoError(t, os.WriteFile(cfg.Telegram.SessionFile, []byte(`{}`), 0o600))
	start, err := newStartup(cfg, key)
	require.NoError(t, err)

	h := testHostRig{delivered: make(chan tea.Msg, 64), endpoints: make(chan internaltg.Endpoint, 8),
		path: path, stateDir: stateDir}
	var mu sync.Mutex
	opened := 0
	h.app = &App{
		cfgStore: config.NewStoreOf(cfg, path, stateDir),
		startup:  start,
		log:      zap.NewNop(),
		notifier: newNotifier(zap.NewNop()),
		tmpDir:   t.TempDir(),
		deliver:  func(msg tea.Msg) { h.delivered <- msg },
		connect: func(endpoint internaltg.Endpoint, _ updates.StateStorage) core.Connection {
			h.endpoints <- endpoint
			mu.Lock()
			defer mu.Unlock()
			opened++
			if opened == 1 {
				return first
			}
			return &heldConnection{updates: make(chan store.Event)}
		},
	}
	_, _, err = h.app.openNext()
	require.NoError(t, err)
	return h
}

func waitForEnd(t *testing.T, delivered chan tea.Msg) ui.AccountEndedMsg {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-delivered:
			if ended, ok := msg.(ui.AccountEndedMsg); ok {
				return ended
			}
		case <-deadline:
			require.Fail(t, "the account never ended")
			return ui.AccountEndedMsg{}
		}
	}
}

// A log out ends the account in the running process: the next account starts
// in its place, with nothing of the last one on disk - not its history, not its
// session - and the client is handed the next one's model with the reason
// (#297).
func TestHost_ALogOutStartsTheNextAccountWithNothingOfTheLast(t *testing.T) {
	a, delivered, sessionFile := testHost(t)
	first, epoch := a.current()
	_, err := first.store.DB().Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('mark', 'first')`)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.launch(ctx, first, epoch)
	ended := waitForEnd(t, delivered)

	assert.Equal(t, epoch+1, ended.Epoch)
	assert.Equal(t, core.AccountEnded{Reason: core.EndLoggedOutElsewhere}, ended.Ended)
	assert.NoFileExists(t, sessionFile, "a login would bind to the old key")

	next, nextEpoch := a.current()
	assert.NotSame(t, first, next)
	assert.Equal(t, ended.Epoch, nextEpoch)
	var v string
	err = next.store.DB().QueryRow(`SELECT value FROM metadata WHERE key = 'mark'`).Scan(&v)
	assert.Error(t, err, "the next account found the last one's data")

	cancel()
	a.switching.Lock()
	next.stop()
	a.switching.Unlock()
}

// A banned account's connection is over before the person decides anything.
// Leaving it to log in with another number is the client's request, and ends
// the account like any log out; what the banned one ended with is no longer
// the process's exit error (#297).
func TestHost_LeavingABannedAccountStartsTheNextOne(t *testing.T) {
	a, delivered, _ := testHostWith(t, &bannedConnection{updates: make(chan store.Event)})
	first, epoch := a.current()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.launch(ctx, first, epoch)
	require.Eventually(t, func() bool { return a.lastExitErr() != nil }, 2*time.Second, 5*time.Millisecond)

	first.owner.EndAccount(core.EndBanned)
	ended := waitForEnd(t, delivered)

	assert.Equal(t, core.EndBanned, ended.Ended.Reason)
	assert.NoError(t, a.lastExitErr())

	cancel()
	next, _ := a.current()
	a.switching.Lock()
	next.stop()
	a.switching.Unlock()
}

// The app key a build carries is the process's, not the file's: an account
// that starts after a log out connects with it even when the config was
// reloaded in between from a file that names no key (#239).
func TestHost_TheNextAccountConnectsWithTheBuiltInKeyAfterAReload(t *testing.T) {
	builtIn := appkey.Key{ID: 4242, Hash: "built-in"}
	h := newTestHost(t, &loggedOutConnection{updates: make(chan store.Event)}, builtIn, "ui:\n  history_limit: 30\n")
	a := h.app
	<-h.endpoints // the first account's
	_, err := a.reloadConfig()
	require.NoError(t, err)

	first, epoch := a.current()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.launch(ctx, first, epoch)
	waitForEnd(t, h.delivered)

	select {
	case endpoint := <-h.endpoints:
		assert.Equal(t, builtIn, endpoint.Key, "the next account connected without the key the build carries")
	case <-time.After(3 * time.Second):
		require.Fail(t, "the next account never connected")
	}

	cancel()
	last, _ := a.current()
	a.switching.Lock()
	last.stop()
	a.switching.Unlock()
}

// Whatever the ended account's bridges still had to say is marked with its
// epoch, so the next account's model drops it.
func TestHost_WhatAnAccountSendsIsMarkedWithItsEpoch(t *testing.T) {
	a, delivered, _ := testHost(t)

	a.send(7, ui.FolderFiltersMsg{})

	assert.Equal(t, ui.Stamped(7, ui.FolderFiltersMsg{}), <-delivered)
}
