package ui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
	"github.com/sorokin-vladimir/tele/internal/ui/screens"
)

// The counter rises on arrival and is cleared only when the server confirms the
// read, so a failed MarkRead leaves a count that agrees with the server instead
// of silently under-reporting (#189).
func TestRoot_OpenChat_UnreadClearedOnMarkReadConfirmation(t *testing.T) {
	m, st := newRootWithTwoChatsInternal(t)

	newM, _ := m.Update(screens.OpenChatMsg{ChatID: 1, Title: "Alice"})
	m = newM.(RootModel)

	newM, _ = applyEventInternal(t, m, st, store.Event{
		Kind:    store.EventNewMessage,
		Message: domain.Message{ID: 7, ChatID: 1, Text: "hi"},
	})
	m = newM.(RootModel)

	c, _ := st.GetChat(1)
	require.Equal(t, 1, c.UnreadCount, "unread is counted before the server confirms the read")

	// Reading is the owner's command now, and it moves the pointer itself.
	require.NoError(t, m.owner.MarkRead(context.Background(), domain.HistoryKey{ChatID: 1}, 7))

	c, _ = st.GetChat(1)
	assert.Equal(t, 0, c.UnreadCount)
}

// A chat can be open while the chat list holds focus; markReadCmd returns nil in
// that state (internal/ui/root_cmds.go:14), so nothing clears the count and the
// message must not be swallowed.
func TestRoot_OpenChatUnfocused_KeepsUnread(t *testing.T) {
	m, st := newRootWithTwoChatsInternal(t)

	newM, _ := m.Update(screens.OpenChatMsg{ChatID: 1, Title: "Alice"})
	m = newM.(RootModel)
	m = m.WithFocus(FocusChatList)

	applyEventInternal(t, m, st, store.Event{
		Kind:    store.EventNewMessage,
		Message: domain.Message{ID: 7, ChatID: 1, Text: "hi"},
	})

	c, _ := st.GetChat(1)
	assert.Equal(t, 1, c.UnreadCount)
}
