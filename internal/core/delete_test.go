package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

// A message deleted from another device must leave the open chat's window.
func TestOwner_DeleteFromAnotherDevice_LeavesTheWindow(t *testing.T) {
	o, events, st := newTestOwner(t)
	st.SetChat(domain.Chat{ID: 1, Title: "Ada", Peer: domain.Peer{ID: 1, Type: domain.PeerUser}})
	st.SetMessages(1, []domain.Message{
		{ID: 10, ChatID: 1, Text: "one", Date: time.Unix(1, 0)},
		{ID: 11, ChatID: 1, Text: "two", Date: time.Unix(2, 0)},
	})
	o.Subscribe(project.HistoryWindow{
		ChatID: 1, Anchor: project.Anchor{Kind: project.AnchorNewest}, Before: 2,
	})
	recvDelta(t, o.Deltas()) // opening Reset
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.RunUpdates(ctx)

	events <- store.Event{Kind: store.EventDeleteMessages, MsgIDs: []int{11}}

	d, ok := recvDelta(t, o.Deltas())
	require.True(t, ok, "the deletion must reach the open chat")
	require.NotNil(t, d.History)
	assert.Equal(t, project.HistoryRemove, d.History.Kind)
	assert.Equal(t, []int{11}, d.History.MsgIDs)
	assert.Len(t, st.Messages(1), 1, "and the store must no longer hold it")
}

// The same, for a message this client sent. It reaches the store the way the
// outbox worker puts it there: under the ID Telegram confirmed, since #195 left
// no client-side renumbering behind.
func TestOwner_DeleteOfAMessageWeSent_LeavesTheWindow(t *testing.T) {
	o, events, st := newTestOwner(t)
	st.SetChat(domain.Chat{ID: 1, Title: "Ada", Peer: domain.Peer{ID: 1, Type: domain.PeerUser}})
	st.AppendMessage(domain.Message{ID: 42, ChatID: 1, Text: "mine", IsOut: true, Date: time.Unix(1, 0)})
	o.Subscribe(project.HistoryWindow{
		ChatID: 1, Anchor: project.Anchor{Kind: project.AnchorNewest}, Before: 5,
	})
	recvDelta(t, o.Deltas())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.RunUpdates(ctx)

	events <- store.Event{Kind: store.EventDeleteMessages, MsgIDs: []int{42}}

	d, ok := recvDelta(t, o.Deltas())
	require.True(t, ok, "deleting a message we sent must reach the open chat")
	require.NotNil(t, d.History)
	assert.Equal(t, project.HistoryRemove, d.History.Kind)
	assert.Empty(t, st.Messages(1))
}
