package tg

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	gotdtg "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// Telegram has already dropped a duplicated key, and every request made with it
// fails the same way. Seeing it once is enough to end the connection, whichever
// request it came back on (#254).
func TestMapError_ADuplicatedKeyEndsTheConnection(t *testing.T) {
	c := testClient()
	ctx, cancel := context.WithCancelCause(context.Background())
	c.loggedOut.arm(cancel)

	err := c.mapError("messages.getDialogs", tgerr.New(406, "AUTH_KEY_DUPLICATED"))

	assert.Equal(t, telerr.Unauthorized, telerr.Of(err))
	assert.ErrorIs(t, context.Cause(ctx), errLoggedOut)
	assert.Equal(t, telerr.LogOutKeyDropped, logOutOfOutcome(t, c))
}

func TestMapError_AnOrdinaryLogOutLeavesTheConnection(t *testing.T) {
	c := testClient()
	ctx, cancel := context.WithCancelCause(context.Background())
	c.loggedOut.arm(cancel)

	_ = c.mapError("users.getUsers", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))

	assert.NoError(t, ctx.Err())
}

func TestLoggedOut_LeavesAnyOtherEndingAlone(t *testing.T) {
	c := testClient()
	ended := errors.New("connection lost")

	assert.Same(t, ended, c.loggedOut.outcome(ended))
}

// logOutOfOutcome is how the connection would say it ended.
func logOutOfOutcome(t *testing.T, c *GotdClient) telerr.LogOut {
	t.Helper()
	e, ok := telerr.As(c.loggedOut.outcome(context.Canceled))
	require.True(t, ok, "the connection did not end as a log out")
	assert.Equal(t, telerr.Unauthorized, e.Kind)
	return e.LogOut
}

// recordingInvoker answers every request with err, or with what errFor names
// for its method, and remembers what it was asked, through the same mapping
// every request goes through.
type recordingInvoker struct {
	mu     sync.Mutex
	err    error
	errFor map[string]error
	asked  []string
}

func (r *recordingInvoker) Invoke(_ context.Context, input bin.Encoder, _ bin.Decoder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op := opName(input)
	r.asked = append(r.asked, op)
	if err, ok := r.errFor[op]; ok {
		return err
	}
	return r.err
}

func (r *recordingInvoker) count(op string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, a := range r.asked {
		if a == op {
			n++
		}
	}
	return n
}

// A 401 while the account is in use is a question, not an answer: a media data
// centre can refuse a session the main one still holds. The main one is asked,
// and only its 401 ends the account, as the log out it reports (#297).
func TestRecheck_ALogOutConfirmedOnTheMainDataCentreEndsTheAccount(t *testing.T) {
	inv := &recordingInvoker{err: tgerr.New(401, "AUTH_KEY_UNREGISTERED")}
	c, ctx := clientOver(inv)
	c.authorized.Store(true)

	_ = c.mapError("upload.getFile", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))

	require.Eventually(t, func() bool { return ctx.Err() != nil }, time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, inv.count("users.getUsers"))
	assert.Equal(t, telerr.LogOutElsewhere, logOutOfOutcome(t, c))
}

func TestRecheck_ASessionTheMainDataCentreStillHoldsGoesOn(t *testing.T) {
	inv := &recordingInvoker{}
	c, ctx := clientOver(inv)
	c.authorized.Store(true)

	_ = c.mapError("upload.getFile", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))

	require.Eventually(t, func() bool { return inv.count("users.getUsers") == 1 }, time.Second, 5*time.Millisecond)
	c.recheck.wait()
	assert.NoError(t, ctx.Err())
}

// A burst of refusals asks once, and a session found alive is not asked about
// again straight away.
func TestRecheck_IsNotAskedOverAndOver(t *testing.T) {
	inv := &recordingInvoker{}
	c, _ := clientOver(inv)
	c.authorized.Store(true)

	for i := 0; i < 5; i++ {
		_ = c.mapError("upload.getFile", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))
	}
	c.recheck.wait()
	_ = c.mapError("upload.getFile", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))
	c.recheck.wait()

	assert.Equal(t, 1, inv.count("users.getUsers"))
}

// Before the login a 401 is what a session with nothing to log out of answers,
// and the login sorts it out.
func TestRecheck_NotBeforeTheLogin(t *testing.T) {
	inv := &recordingInvoker{}
	c, _ := clientOver(inv)

	_ = c.mapError("users.getUsers", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))
	c.recheck.wait()

	assert.Zero(t, inv.count("users.getUsers"))
}

func clientOver(inv *recordingInvoker) (*GotdClient, context.Context) {
	c := testClient()
	c.api = gotdtg.NewClient(c.errorMiddleware().Handle(inv))
	ctx, cancel := context.WithCancelCause(context.Background())
	c.loggedOut.arm(cancel)
	c.recheck.arm(ctx)
	return c, ctx
}

// Logging out tells Telegram first, so the session leaves the person's devices
// list, and then ends the connection as a log out made here (#297).
func TestLogOut_TellsTelegramThenEndsTheConnection(t *testing.T) {
	inv := &recordingInvoker{}
	c, ctx := clientOver(inv)

	require.NoError(t, c.LogOut(context.Background(), true))

	assert.Equal(t, []string{"auth.logOut"}, inv.asked)
	assert.ErrorIs(t, context.Cause(ctx), errLoggedOut)
	assert.Equal(t, telerr.LogOutHere, logOutOfOutcome(t, c))
}

// Telegram out of reach is the person's to decide about: nothing ends, and the
// error says why, so they can choose to log out here anyway.
func TestLogOut_TelegramOutOfReachEndsNothing(t *testing.T) {
	c, ctx := clientOver(&recordingInvoker{err: errors.New("dial tcp: connection refused")})

	err := c.LogOut(context.Background(), true)

	require.Error(t, err)
	assert.NoError(t, ctx.Err())
}

// A session Telegram has already logged out has nothing to tell it.
func TestLogOut_AnAlreadyEndedSessionStillLogsOut(t *testing.T) {
	c, ctx := clientOver(&recordingInvoker{err: tgerr.New(401, "AUTH_KEY_UNREGISTERED")})

	require.NoError(t, c.LogOut(context.Background(), true))

	assert.Error(t, ctx.Err())
	assert.Equal(t, telerr.LogOutHere, logOutOfOutcome(t, c))
}

// Logging out here anyway asks Telegram nothing.
func TestLogOut_HereOnly(t *testing.T) {
	inv := &recordingInvoker{}
	c, ctx := clientOver(inv)

	require.NoError(t, c.LogOut(context.Background(), false))

	assert.Empty(t, inv.asked)
	assert.Error(t, ctx.Err())
	assert.Equal(t, telerr.LogOutHere, logOutOfOutcome(t, c))
}
