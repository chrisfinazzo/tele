package store_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

func TestApplyIncomingMessage_AppendsAndCounts(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "A"})

	isNew, counted := store.ApplyIncomingMessage(s, domain.Message{ID: 5, ChatID: 1, Text: "hi"})
	assert.True(t, isNew)
	assert.True(t, counted)

	msgs := s.Messages(domain.HistoryKey{ChatID: 1})
	require.Len(t, msgs, 1)
	assert.Equal(t, 5, msgs[0].ID)
	c, _ := s.GetChat(1)
	assert.Equal(t, 1, c.UnreadCount)
}

func TestApplyIncomingMessage_OutgoingAppendsWithoutCounting(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1})

	isNew, counted := store.ApplyIncomingMessage(s, domain.Message{ID: 5, ChatID: 1, IsOut: true})
	assert.True(t, isNew)
	assert.False(t, counted)

	require.Len(t, s.Messages(domain.HistoryKey{ChatID: 1}), 1)
	c, _ := s.GetChat(1)
	assert.Equal(t, 0, c.UnreadCount)
}

func TestApplyIncomingMessage_CountsMention(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1})

	_, counted := store.ApplyIncomingMessage(s, domain.Message{ID: 5, ChatID: 1, Mentioned: true})
	assert.True(t, counted)

	c, _ := s.GetChat(1)
	assert.Equal(t, 1, c.UnreadCount)
	assert.Equal(t, 1, c.UnreadMentionsCount)
}

// A message already covered by the read pointer still belongs in history, but
// changes no counter — so the caller must not be told to refresh derived views.
func TestApplyIncomingMessage_ReadElsewhereAppendsAndReportsNoChange(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, ReadInboxMaxID: 10})

	isNew, counted := store.ApplyIncomingMessage(s, domain.Message{ID: 5, ChatID: 1})
	assert.True(t, isNew, "it still belongs in history")
	assert.False(t, counted)

	require.Len(t, s.Messages(domain.HistoryKey{ChatID: 1}), 1)
	c, _ := s.GetChat(1)
	assert.Equal(t, 0, c.UnreadCount)
}

// The helper takes no viewport: the same message produces the same state
// regardless of what any client has open. This is what #183 requires.
func TestApplyIncomingMessage_ReplayCountsOnce(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1})
	msg := domain.Message{ID: 5, ChatID: 1, Mentioned: true}

	isNew, counted := store.ApplyIncomingMessage(s, msg)
	require.True(t, isNew)
	require.True(t, counted)

	isNew, counted = store.ApplyIncomingMessage(s, msg)
	assert.False(t, isNew, "the same message again is not a second arrival")
	assert.False(t, counted)

	c, _ := s.GetChat(1)
	assert.Equal(t, 1, c.UnreadCount)
	assert.Equal(t, 1, c.UnreadMentionsCount)
}
