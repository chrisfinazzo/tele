package tg

import (
	"context"
	"errors"
	"sync"
	"time"

	gotdtg "github.com/gotd/td/tg"
	"go.uber.org/zap"

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

// recheckCooldown is how long a session found alive is not asked about again.
// A media data centre can go on refusing a session the main one holds, and
// each refusal would otherwise ask again.
const recheckCooldown = 30 * time.Second

// recheck asks the main data centre whether the session still holds, when a
// request was refused as unauthorized while the account is in use (#297).
//
// A 401 is a question rather than an answer: gotd carries the authorization to
// each media data centre separately, and a refusal there says nothing about the
// session on the main one. Ending the account removes its history, so it ends
// only on the main data centre's word. One check runs at a time - its own
// refusal, which comes back through the same mapping, does not start another -
// and a session found alive is not asked about again for recheckCooldown.
//
// The zero value is ready to use once armed.
type recheck struct {
	mu      sync.Mutex
	ctx     context.Context
	running bool
	aliveAt time.Time
	work    sync.WaitGroup
}

// arm sets the connection's context, which bounds every check.
func (r *recheck) arm(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ctx = ctx
}

// start runs check unless one is running, the session was found alive lately,
// or there is no connection to check on. check reports whether the session was
// found alive.
func (r *recheck) start(check func(context.Context) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx == nil || r.running || time.Since(r.aliveAt) < recheckCooldown {
		return
	}
	r.running = true
	r.work.Add(1)
	ctx := r.ctx
	go func() {
		defer r.work.Done()
		alive := check(ctx)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.running = false
		if alive {
			r.aliveAt = time.Now()
		}
	}()
}

// wait returns once no check is running.
func (r *recheck) wait() { r.work.Wait() }

// checkSession asks the main data centre who is logged in. Its 401 is the log
// out it names, and ends the account; anything else leaves it.
func (c *GotdClient) checkSession(ctx context.Context) bool {
	api, err := c.acquireAPI()
	if err != nil {
		return false
	}
	_, err = api.UsersGetUsers(ctx, []gotdtg.InputUserClass{&gotdtg.InputUserSelf{}})
	if e, ok := telerr.As(err); ok && e.Kind == telerr.Unauthorized {
		c.log.Warn("the main data centre confirmed the log out", zap.String("type", e.Detail))
		c.loggedOut.report(e.LogOut)
		return false
	}
	return err == nil
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
