package tg

import (
	"context"
	"errors"
	"os"
	"sync"

	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// errKeyDropped is why the connection is ended when Telegram drops the key.
var errKeyDropped = errors.New("telegram invalidated the session's key")

// droppedKey ends the connection once Telegram reports AUTH_KEY_DUPLICATED, and
// removes the session after it.
//
// Telegram sends that error when one key is used from two connections at once,
// and by then it has already invalidated the session: no request made with the
// key will succeed again, a login included, until a new key replaces it (#254).
// The session file is removed only once the connection is over, because gotd
// keeps the key in memory and may write it back while it runs. Removing the file
// is enough: the next start finds no session, removes the account's footprint
// as it does for any missing session, and gotd makes a new key for the login.
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
// dropped, in which case the session is removed and the error says so.
func (d *droppedKey) outcome(err error, sess *fileSession, log *zap.Logger) error {
	d.mu.Lock()
	seen := d.seen
	d.mu.Unlock()
	if !seen {
		return err
	}
	dropped := &telerr.Error{Kind: telerr.Unauthorized, Op: "session", Detail: "AUTH_KEY_DUPLICATED"}
	if rmErr := os.Remove(sess.inner.Path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		// Without the removal a restart meets the same dead key, so the error
		// is not allowed to claim it happened.
		log.Error("dropped session could not be removed", zap.String("path", sess.inner.Path), zap.Error(rmErr))
		dropped.Cause = rmErr
		return dropped
	}
	log.Warn("telegram invalidated the session's key; session removed", zap.String("path", sess.inner.Path))
	dropped.SessionRemoved = true
	return dropped
}
