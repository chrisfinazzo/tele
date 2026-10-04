package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/telerr"
)

func (s *stubClient) SetTyping(_ context.Context, _ domain.Peer, topic int, _ domain.TypingAction) error {
	s.typingCalls++
	s.typingTopic = topic
	return s.err
}

func (s *stubClient) SaveDraft(_ context.Context, _ domain.Peer, topic int, text string) error {
	s.draftText = text
	s.draftTopic = topic
	return s.err
}

func TestSaveDraft_StoresTheDraftOnSuccess(t *testing.T) {
	c := &stubClient{}
	o, st := newCmdOwner(t, c)

	require.NoError(t, o.SaveDraft(context.Background(), domain.HistoryKey{ChatID: 1}, "unsent"))

	chat, _ := st.GetChat(1)
	assert.Equal(t, "unsent", chat.Draft)
	assert.Equal(t, "unsent", c.draftText)
}

// The draft is what the composer holds; a failed sync must not erase it from
// the local chat, or reopening would lose typed text.
func TestSaveDraft_KeepsTheLocalDraftWhenTheSyncFails(t *testing.T) {
	c := &stubClient{err: &telerr.Error{Kind: telerr.Network}}
	o, st := newCmdOwner(t, c)

	require.Error(t, o.SaveDraft(context.Background(), domain.HistoryKey{ChatID: 1}, "unsent"))

	chat, _ := st.GetChat(1)
	assert.Equal(t, "unsent", chat.Draft)
}

func TestSetTyping_ReturnsTheErrorWithoutTouchingState(t *testing.T) {
	c := &stubClient{err: &telerr.Error{Kind: telerr.Network}}
	o, _ := newCmdOwner(t, c)

	assert.Error(t, o.SetTyping(context.Background(), domain.HistoryKey{ChatID: 1}, domain.TypingActionTyping))
	assert.Equal(t, 1, c.typingCalls)
}

func TestSetTyping_UnknownChatIsPeerNotFound(t *testing.T) {
	c := &stubClient{}
	o, _ := newCmdOwner(t, c)

	err := o.SetTyping(context.Background(), domain.HistoryKey{ChatID: 404}, domain.TypingActionTyping)

	assert.Equal(t, telerr.PeerNotFound, telerr.Of(err))
	assert.Equal(t, 0, c.typingCalls, "an unknown chat never reaches Telegram")
}
