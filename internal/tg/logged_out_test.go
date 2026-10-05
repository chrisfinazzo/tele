package tg

import (
	"context"
	"errors"
	"testing"

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

// recordingInvoker answers every request with err, and remembers what it was
// asked, through the same mapping every request goes through.
type recordingInvoker struct {
	err   error
	asked []string
}

func (r *recordingInvoker) Invoke(_ context.Context, input bin.Encoder, _ bin.Decoder) error {
	r.asked = append(r.asked, opName(input))
	return r.err
}

func clientOver(inv *recordingInvoker) (*GotdClient, context.Context) {
	c := testClient()
	c.api = gotdtg.NewClient(c.errorMiddleware().Handle(inv))
	ctx, cancel := context.WithCancelCause(context.Background())
	c.loggedOut.arm(cancel)
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
