package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/ui/components"
)

// A reaction Telegram accepted but did not keep is reported rather than lost
// in silence (#248). The set on screen is already the one Telegram sent, so
// there is nothing to roll back: a warning, not an error.
func TestReactConfirmed_NotKeptRaisesAWarning(t *testing.T) {
	o, m := openTestChat(t, project.HistoryContents{
		Messages: []domain.Message{{ID: 5, ChatID: 1, Text: "hi", Date: time.Unix(1, 0)}},
	})
	o.reactionNotKept = true
	m.reactionTargetID = 5
	require.True(t, m.toasts.Empty())

	nm, cmd := m.Update(components.ReactConfirmedMsg{Emoji: "👍"})
	m = nm.(RootModel)
	require.NotNil(t, cmd)
	msg := cmd()
	notKept, ok := msg.(reactionNotKeptMsg)
	require.True(t, ok, "a reaction that was not kept is its own outcome, got %T", msg)
	assert.Equal(t, 5, notKept.msgID)

	nm, _ = m.Update(msg)
	assert.False(t, nm.(RootModel).toasts.Empty(), "the person is told")
}

func TestReactConfirmed_KeptSaysNothing(t *testing.T) {
	_, m := openTestChat(t, project.HistoryContents{
		Messages: []domain.Message{{ID: 5, ChatID: 1, Text: "hi", Date: time.Unix(1, 0)}},
	})
	m.reactionTargetID = 5

	_, cmd := m.Update(components.ReactConfirmedMsg{Emoji: "👍"})
	require.NotNil(t, cmd)
	assert.Nil(t, cmd())
}
