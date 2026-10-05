package tg

import (
	"context"
	"errors"
	"sync"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// errLoggedOut is why the connection is ended when the account is logged out.
var errLoggedOut = errors.New("the account was logged out")

// loggedOut ends the connection as a log out, and remembers which.
//
// Two log outs end it from inside a running connection. The person logs out
// here. Or Telegram reports AUTH_KEY_DUPLICATED: one key was used from two
// connections at once, and by then Telegram has already invalidated the
// session, so no request made with the key will succeed again, a login
// included, until a new key replaces it (#254). Either way the connection ends
// as a log out, and the host ends the account with it, session included; that
// happens only once the connection is over, because gotd keeps the key in
// memory and may write it back while it runs (#297).
//
// The zero value is ready to use.
type loggedOut struct {
	mu     sync.Mutex
	cause  telerr.LogOut
	cancel context.CancelCauseFunc
}

// arm installs what ends the connection. Set before the connection runs.
func (l *loggedOut) arm(cancel context.CancelCauseFunc) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cancel = cancel
}

// report records how the account was logged out and ends the connection. The
// first report is the one kept.
func (l *loggedOut) report(cause telerr.LogOut) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cause == "" {
		l.cause = cause
	}
	if l.cancel != nil {
		l.cancel(errLoggedOut)
	}
}

// outcome is what the connection ended with: err as it is, unless the account
// was logged out, which ends it as that log out whatever the connection itself
// said.
func (l *loggedOut) outcome(err error) error {
	l.mu.Lock()
	cause := l.cause
	l.mu.Unlock()
	if cause == "" {
		return err
	}
	return &telerr.Error{Kind: telerr.Unauthorized, Op: "session", LogOut: cause, Cause: err}
}

// LogOut logs the account out. With tellTelegram it asks Telegram to end the
// session first, so it leaves the person's devices list; if Telegram cannot be
// reached, nothing ends and the error says why, which is the person's to
// decide about. A session Telegram already ended has nothing to be told. Then
// the connection ends as a log out made here, and the host ends the account
// (#297).
func (c *GotdClient) LogOut(ctx context.Context, tellTelegram bool) error {
	if tellTelegram {
		api, err := c.acquireAPI()
		if err != nil {
			return err
		}
		if _, err := api.AuthLogOut(ctx); err != nil && telerr.Of(err) != telerr.Unauthorized {
			return err
		}
	}
	c.loggedOut.report(telerr.LogOutHere)
	return nil
}
