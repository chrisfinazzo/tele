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

func (c *loggedOutConnection) Connect(context.Context, *config.Config, *internaltg.AuthFlow,
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

func (c *bannedConnection) Connect(context.Context, *config.Config, *internaltg.AuthFlow,
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
	stateDir := t.TempDir()
	cfg := &config.Config{StateDir: stateDir}
	cfg.Telegram.SessionFile = filepath.Join(stateDir, "session.json")
	require.NoError(t, os.WriteFile(cfg.Telegram.SessionFile, []byte(`{}`), 0o600))

	delivered := make(chan tea.Msg, 64)
	var mu sync.Mutex
	opened := 0
	a := &App{
		cfgStore: config.NewStoreOf(cfg, filepath.Join(stateDir, "config.yml"), stateDir),
		log:      zap.NewNop(),
		notifier: newNotifier(zap.NewNop()),
		tmpDir:   t.TempDir(),
		deliver:  func(msg tea.Msg) { delivered <- msg },
		connect: func(updates.StateStorage) core.Connection {
			mu.Lock()
			defer mu.Unlock()
			opened++
			if opened == 1 {
				return first
			}
			return &heldConnection{updates: make(chan store.Event)}
		},
	}
	_, _, err := a.openNext()
	require.NoError(t, err)
	return a, delivered, cfg.Telegram.SessionFile
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

// Whatever the ended account's bridges still had to say is marked with its
// epoch, so the next account's model drops it.
func TestHost_WhatAnAccountSendsIsMarkedWithItsEpoch(t *testing.T) {
	a, delivered, _ := testHost(t)

	a.send(7, ui.FolderFiltersMsg{})

	assert.Equal(t, ui.Stamped(7, ui.FolderFiltersMsg{}), <-delivered)
}
