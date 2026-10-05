package tg

import (
	"context"
	"errors"
	"testing"

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
	c.dropped.arm(cancel)

	err := c.mapError("messages.getDialogs", tgerr.New(406, "AUTH_KEY_DUPLICATED"))

	assert.Equal(t, telerr.Unauthorized, telerr.Of(err))
	assert.ErrorIs(t, context.Cause(ctx), errKeyDropped)
}

func TestMapError_AnOrdinaryLogOutLeavesTheConnection(t *testing.T) {
	c := testClient()
	ctx, cancel := context.WithCancelCause(context.Background())
	c.dropped.arm(cancel)

	_ = c.mapError("users.getUsers", tgerr.New(401, "AUTH_KEY_UNREGISTERED"))

	assert.NoError(t, ctx.Err())
}

// The connection ends with the account: what it ended with says the key went,
// and the host takes the session with the rest of the account (#297).
func TestDroppedKey_EndsTheConnectionAsALogOut(t *testing.T) {
	c := testClient()
	c.dropped.report()

	err := c.dropped.outcome(context.Canceled)

	e, ok := telerr.As(err)
	require.True(t, ok)
	assert.Equal(t, telerr.Unauthorized, e.Kind)
	assert.Equal(t, telerr.LogOutKeyDropped, e.LogOut)
}

func TestDroppedKey_LeavesAnyOtherEndingAlone(t *testing.T) {
	c := testClient()
	ended := errors.New("connection lost")

	assert.Same(t, ended, c.dropped.outcome(ended))
}
