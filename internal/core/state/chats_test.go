package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/state"
	"github.com/sorokin-vladimir/tele/internal/domain"
)

func TestApplyReadInbox_AdvancesAndReportsChange(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1, ReadInboxMaxID: 5})

	chg, ok := s.ApplyReadInbox(1, 10)

	require.True(t, ok)
	assert.Equal(t, state.ChangeReadInbox, chg.Kind)
	assert.Equal(t, int64(1), chg.ChatID)
	c, _ := st.GetChat(1)
	assert.Equal(t, 10, c.ReadInboxMaxID)
}

// A pointer that does not advance must produce no change at all, so no client
// is woken. This is the gate internal/ui/root_store_events.go:54 performs today.
func TestApplyReadInbox_NoAdvanceIsNoop(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1, ReadInboxMaxID: 10})

	_, ok := s.ApplyReadInbox(1, 10)

	assert.False(t, ok)
	c, _ := st.GetChat(1)
	assert.Equal(t, 10, c.ReadInboxMaxID)
}

func TestApplyReadOutbox_AdvancesAndReportsChange(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1, ReadOutboxMaxID: 5})

	chg, ok := s.ApplyReadOutbox(1, 10)

	require.True(t, ok)
	assert.Equal(t, state.ChangeReadOutbox, chg.Kind)
	c, _ := st.GetChat(1)
	assert.Equal(t, 10, c.ReadOutboxMaxID)
}

func TestApplyReadOutbox_NoAdvanceIsNoop(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1, ReadOutboxMaxID: 10})

	_, ok := s.ApplyReadOutbox(1, 10)

	assert.False(t, ok)
}

func TestApplyPresence_FlipReportsChange(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1})

	chg, ok := s.ApplyPresence(1, true)

	require.True(t, ok)
	assert.Equal(t, state.ChangePresence, chg.Kind)
	assert.True(t, chg.Online)
	c, _ := st.GetChat(1)
	assert.True(t, c.Online)
}

// Presence updates stream continuously for every online contact. An unchanged
// state must cost nothing downstream.
func TestApplyPresence_SameStateIsNoop(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1, Online: true})

	_, ok := s.ApplyPresence(1, true)

	assert.False(t, ok)
}

func TestApplyMute_FlipReportsChange(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1})

	chg, ok := s.ApplyMute(1, true)

	require.True(t, ok)
	assert.Equal(t, state.ChangeMute, chg.Kind)
	assert.True(t, chg.Muted)
	c, _ := st.GetChat(1)
	assert.True(t, c.IsMuted)
}

func TestApplyMute_SameStateIsNoop(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1, IsMuted: true})

	_, ok := s.ApplyMute(1, true)

	assert.False(t, ok)
}

func TestApplyMute_UnknownChatIsNoop(t *testing.T) {
	s, _ := newState(t)

	_, ok := s.ApplyMute(42, true)

	assert.False(t, ok)
}

func TestApplyDraft_StoresAndReportsChange(t *testing.T) {
	s, st := newState(t)
	st.SetChat(domain.Chat{ID: 1})

	chg, ok := s.ApplyDraft(1, 0, "hello")

	require.True(t, ok)
	assert.Equal(t, state.ChangeDraft, chg.Kind)
	assert.Equal(t, "hello", chg.Draft)
	c, _ := st.GetChat(1)
	assert.Equal(t, "hello", c.Draft)
}

// Typing has no persisted representation; it is published straight through.
func TestApplyTyping_IsEphemeralPassThrough(t *testing.T) {
	s, _ := newState(t)

	chg, ok := s.ApplyTyping(1, 0, domain.TypingActionTyping)

	require.True(t, ok)
	assert.Equal(t, state.ChangeTyping, chg.Kind)
	assert.Equal(t, int64(1), chg.ChatID)
	assert.Equal(t, domain.TypingActionTyping, chg.Typing)
}
