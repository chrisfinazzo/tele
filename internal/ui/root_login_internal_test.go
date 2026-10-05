package ui

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// A connection that ended for good does not come back by waiting, so its toast
// stays until someone dismisses it: one that timed out would leave a dead app
// with nothing on screen saying so (#283).
func TestConnectFailed_AfterLoginToastDoesNotExpire(t *testing.T) {
	m := mainScreenModel()

	next, cmd := m.handleConnectFailed(ConnectFailedMsg{Err: &telerr.Error{Kind: telerr.Network}})

	assert.False(t, next.toasts.Empty())
	assert.Nil(t, cmd, "no clear may be scheduled for this toast")
}

// A removed session is no failure to connect either: the toast says what went,
// what the next start removes, what stays, and that a restart logs in (#254).
func TestConnectFailed_ARemovedSessionSaysWhatWasRemoved(t *testing.T) {
	m := mainScreenModel()

	next, _ := m.handleConnectFailed(ConnectFailedMsg{Err: &telerr.Error{
		Kind: telerr.Unauthorized, Detail: "AUTH_KEY_DUPLICATED", SessionRemoved: true,
	}})

	// The toast wraps inside a border, so words are compared rather than lines.
	text := strings.Join(strings.Fields(strings.ReplaceAll(xansi.Strip(onScreen(next)), "│", " ")), " ")
	assert.Contains(t, text, "Telegram invalidated this session")
	assert.Contains(t, text, "Restart tele to log in.")
	assert.NotContains(t, text, "Could not connect")
}
