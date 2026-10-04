package core

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/core/state"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

func gapMsgs(from, to int) []domain.Message {
	out := make([]domain.Message, 0, to-from+1)
	for id := from; id <= to; id++ {
		out = append(out, domain.Message{ID: id, ChatID: 7, Date: time.Unix(int64(id), 0)})
	}
	return out
}

// openChatWithGap sets up the state #261 leaves behind: a chat holding history
// up to some point, a recorded hole after it, and a server that has moved on.
func openChatWithGap(t *testing.T, c *stubConn, held []domain.Message, top int) (*Owner, *state.State) {
	t.Helper()
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7}, TopMessageID: top})
	s.Store().SetMessages(7, held)
	s.Store().MarkGap(7, held[len(held)-1].ID)
	return o, s
}

func openWindow(o *Owner) project.SubID {
	return o.Subscribe(project.HistoryWindow{
		ChatID: 7, Anchor: project.Anchor{Kind: project.AnchorNewest}, Before: 20,
	})
}

func gapClosed(s *state.State) func() bool {
	return func() bool {
		_, open := s.Store().Gap(7)
		return !open
	}
}

func TestOwner_ForwardFillClosesAGapWhenTheChatOpens(t *testing.T) {
	c := &stubConn{server: gapMsgs(1, 7)}
	o, s := openChatWithGap(t, c, gapMsgs(1, 5), 7)

	openWindow(o)

	require.Eventually(t, gapClosed(s), time.Second, time.Millisecond)
	assert.Len(t, s.Store().Messages(7), 7, "the missed range is in the chat")
}

// A hole wider than one page is closed a page at a time, each one joining the
// last, until the repair reaches where the server was.
func TestOwner_ForwardFillPagesUntilItCatchesUp(t *testing.T) {
	c := &stubConn{server: gapMsgs(1, 45)}
	o, s := openChatWithGap(t, c, gapMsgs(1, 5), 45)

	openWindow(o)

	require.Eventually(t, gapClosed(s), time.Second, time.Millisecond)
	assert.Len(t, s.Store().Messages(7), 45)
	assert.Equal(t, int32(2), c.fwdCalls.Load(), "forty messages at a limit of twenty")
}

// Telegram answers a request for something newer than the newest by sliding the
// window back over messages the caller already has. A repair that read that as
// progress would ask again forever.
func TestOwner_ForwardFillClosesAGapWithNothingBehindIt(t *testing.T) {
	c := &stubConn{server: gapMsgs(1, 5)}
	o, s := openChatWithGap(t, c, gapMsgs(1, 5), 5)

	openWindow(o)

	require.Eventually(t, gapClosed(s), time.Second, time.Millisecond)
	assert.Equal(t, int32(1), c.fwdCalls.Load())
	assert.Len(t, s.Store().Messages(7), 5)
}

// Nobody asked for the repair, so a failure is not reported and not retried on
// a timer. The record stays and the next trigger picks it up.
func TestOwner_ForwardFillKeepsTheGapWhenTheFetchFails(t *testing.T) {
	c := &stubConn{server: gapMsgs(1, 7), fwdErr: errors.New("no connection")}
	o, s := openChatWithGap(t, c, gapMsgs(1, 5), 7)

	openWindow(o)

	require.Eventually(t, func() bool { return c.fwdCalls.Load() >= 1 }, time.Second, time.Millisecond)
	after, open := s.Store().Gap(7)
	require.True(t, open, "a gap that could not be closed is still a gap")
	assert.Equal(t, 5, after, "and it still opens where it did")
}

// Past the point where the chat could hold what is missing, closing the hole
// only evicts what it just fetched. The stored history is dropped for a fresh
// page instead, which is contiguous by construction.
func TestOwner_ForwardFillReloadsTheTailWhenTheGapIsTooWide(t *testing.T) {
	server := gapMsgs(1, 5+store.MaxMessagesPerChat+100)
	c := &stubConn{server: server, history: server[len(server)-20:]}
	o, s := openChatWithGap(t, c, gapMsgs(1, 5), server[len(server)-1].ID)

	openWindow(o)

	require.Eventually(t, gapClosed(s), 5*time.Second, time.Millisecond)
	held := s.Store().Messages(7)
	require.Len(t, held, 20, "the fresh page replaced the history it could not join")
	assert.Equal(t, server[len(server)-1].ID, held[len(held)-1].ID)
}

// A chat with no hole in it and a window the store can fill asks Telegram
// nothing.
func TestOwner_NoGapAndAFullWindowFetchesNothing(t *testing.T) {
	c := &stubConn{}
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7}})
	s.Store().SetMessages(7, gapMsgs(1, 5))

	o.Subscribe(project.HistoryWindow{
		ChatID: 7, Anchor: project.Anchor{Kind: project.AnchorNewest}, Before: 1,
	})

	assert.Zero(t, c.fwdCalls.Load())
	assert.Zero(t, c.calls.Load())
}
