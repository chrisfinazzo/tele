package tg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// The session file goes only once the connection is over, since gotd may still
// write the dead key back while it runs. What is left to say is that it went.
func TestDroppedKey_RemovesTheSessionOnceTheConnectionEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	c := testClient()
	c.dropped.report()

	err := c.dropped.outcome(context.Canceled, NewFileSession(path), c.log)

	e, ok := telerr.As(err)
	require.True(t, ok)
	assert.Equal(t, telerr.Unauthorized, e.Kind)
	assert.True(t, e.SessionRemoved)
	assert.NoFileExists(t, path)
}

func TestDroppedKey_LeavesAnyOtherEndingAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	c := testClient()
	ended := errors.New("connection lost")

	err := c.dropped.outcome(ended, NewFileSession(path), c.log)

	assert.Same(t, ended, err)
	assert.FileExists(t, path)
}

// A key Telegram has dropped cannot log in, so the login is not offered: the
// connection is already ending (#254).
func TestAuthorize_ADuplicatedKeyIsOfferedNoLogin(t *testing.T) {
	af := NewAuthFlow()
	self := &scriptedSelf{errs: []error{refused(406, "AUTH_KEY_DUPLICATED")}}
	client := &scriptedLogin{}

	_, err := af.authorize(context.Background(), self.self, client, true)

	require.Error(t, err)
	assert.Zero(t, client.sends)
}
