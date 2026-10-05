package tg

import (
	"context"
	"errors"
	"sync"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// errKeyDropped is why the connection is ended when Telegram drops the key.
var errKeyDropped = errors.New("telegram invalidated the session's key")

// droppedKey ends the connection once Telegram reports AUTH_KEY_DUPLICATED.
//
// Telegram sends that error when one key is used from two connections at once,
// and by then it has already invalidated the session: no request made with the
// key will succeed again, a login included, until a new key replaces it (#254).
// The connection ends as a log out, and the host ends the account with it,
// session included; that happens only once the connection is over, because
// gotd keeps the key in memory and may write it back while it runs (#297).
//
// The zero value is ready to use.
type droppedKey struct {
	mu     sync.Mutex
	seen   bool
	cancel context.CancelCauseFunc
}

// arm installs what ends the connection. Set before the connection runs.
func (d *droppedKey) arm(cancel context.CancelCauseFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancel = cancel
}

// report records that Telegram dropped the key and ends the connection.
func (d *droppedKey) report() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = true
	if d.cancel != nil {
		d.cancel(errKeyDropped)
	}
}

// outcome is what the connection ended with: err as it is, unless the key was
// dropped, which ends it as a log out whatever the connection itself said.
func (d *droppedKey) outcome(err error) error {
	d.mu.Lock()
	seen := d.seen
	d.mu.Unlock()
	if !seen {
		return err
	}
	return &telerr.Error{
		Kind: telerr.Unauthorized, Op: "session", Detail: "AUTH_KEY_DUPLICATED",
		LogOut: telerr.LogOutKeyDropped, Cause: err,
	}
}
