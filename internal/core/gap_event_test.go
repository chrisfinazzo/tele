package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

// Telegram refuses to say what a channel missed. The position is written down
// at that moment because it stops being knowable as soon as the stream resumes:
// the next arriving message lands on the tail and the chat reads as current.
func TestOwner_ChannelGapIsRecordedAtTheTail(t *testing.T) {
	c := &stubConn{}
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7}})
	s.Store().SetMessages(7, gapMsgs(1, 5))

	o.handleEvent(store.Event{Kind: store.EventChannelGap, ChatID: 7})

	after, open := s.Store().Gap(7)
	require.True(t, open)
	assert.Equal(t, 5, after)
}

// A chat holding nothing has no hole after anything. What it is missing is
// history nobody fetched, and fetching it is backfill's job.
func TestOwner_ChannelGapOnAChatHoldingNothingRecordsNothing(t *testing.T) {
	c := &stubConn{}
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7}})

	o.handleEvent(store.Event{Kind: store.EventChannelGap, ChatID: 7})

	_, open := s.Store().Gap(7)
	assert.False(t, open)
}

// The chat somebody is reading is the one the stall is visible in, so its
// repair does not wait for the chat to be opened again.
func TestOwner_ChannelGapRepairsTheFocusedChatAtOnce(t *testing.T) {
	c := &stubConn{server: gapMsgs(1, 9)}
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7}, TopMessageID: 9})
	s.Store().SetMessages(7, gapMsgs(1, 5))
	a := o.Attach()
	defer a.Detach()
	a.SetFocus(domain.HistoryKey{ChatID: 7})

	o.handleEvent(store.Event{Kind: store.EventChannelGap, ChatID: 7})

	require.Eventually(t, gapClosed(s), time.Second, time.Millisecond)
	assert.Len(t, s.Store().Messages(domain.HistoryKey{ChatID: 7}), 9, "the reader sees the missed range without reopening")
}

func TestOwner_ChannelGapOnAnUnfocusedChatOnlyRecordsIt(t *testing.T) {
	c := &stubConn{server: gapMsgs(1, 9)}
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7}, TopMessageID: 9})
	s.Store().SetMessages(7, gapMsgs(1, 5))

	o.handleEvent(store.Event{Kind: store.EventChannelGap, ChatID: 7})

	assert.Zero(t, c.fwdCalls.Load(), "a chat nobody is reading waits to be opened")
	_, open := s.Store().Gap(7)
	assert.True(t, open)
}

// The account-wide version names no chat, so the dialog list is what answers
// the question: a chat whose tail falls short of where the server says it ends
// missed the difference.
func TestOwner_GapScanMarksTheChatsThatFellBehind(t *testing.T) {
	c := &stubConn{dialogs: []domain.Chat{
		{ID: 7, Peer: domain.Peer{ID: 7}, TopMessageID: 40},
		{ID: 8, Peer: domain.Peer{ID: 8}, TopMessageID: 5},
		{ID: 9, Peer: domain.Peer{ID: 9}, TopMessageID: 12},
	}}
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7}})
	s.Store().SetChat(domain.Chat{ID: 8, Peer: domain.Peer{ID: 8}})
	s.Store().SetChat(domain.Chat{ID: 9, Peer: domain.Peer{ID: 9}})
	s.Store().SetMessages(7, gapMsgs(1, 5))
	s.Store().SetMessages(8, gapMsgs(1, 5))

	o.scanForGaps(t.Context())

	after, open := s.Store().Gap(7)
	require.True(t, open, "the server moved past what this chat holds")
	assert.Equal(t, 5, after)

	_, open = s.Store().Gap(8)
	assert.False(t, open, "this chat is up to date")

	_, open = s.Store().Gap(9)
	assert.False(t, open, "a chat holding nothing has no hole in it")
}
