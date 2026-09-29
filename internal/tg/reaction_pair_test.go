package tg

import (
	"context"
	"testing"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/sorokin-vladimir/tele/internal/store"
)

// editWithReactions is a hidden edit of msgID in a channel, carrying set.
func editWithReactions(channelID int64, msgID, pts int, set tg.MessageReactions) *tg.UpdateEditChannelMessage {
	m := &tg.Message{ID: msgID, PeerID: &tg.PeerChannel{ChannelID: channelID}, EditHide: true, EditDate: 1700000000}
	m.SetReactions(set)
	return &tg.UpdateEditChannelMessage{Message: m, Pts: pts, PtsCount: 1}
}

func reactionsUpdate(channelID int64, msgID int, set tg.MessageReactions) *tg.UpdateMessageReactions {
	return &tg.UpdateMessageReactions{Peer: &tg.PeerChannel{ChannelID: channelID}, MsgID: msgID, Reactions: set}
}

var ourOK = tg.MessageReactions{Results: []tg.ReactionCount{
	{Reaction: &tg.ReactionEmoji{Emoticon: "👌"}, Count: 1},
}}

// passHook runs one envelope through the hook and returns what reached the
// next handler, along with the reaction trace.
func passHook(t *testing.T, u tg.UpdatesClass) (tg.UpdatesClass, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	var got tg.UpdatesClass
	next := telegram.UpdateHandlerFunc(func(_ context.Context, u tg.UpdatesClass) error {
		got = u
		return nil
	})
	h := newOutboxHook(next, make(chan store.Event, 1), zap.New(core))
	require.NoError(t, h.Handle(context.Background(), u))
	return got, logs
}

func editedMessage(t *testing.T, u tg.UpdatesClass, i int) *tg.Message {
	t.Helper()
	upds := u.(*tg.Updates).Updates
	edit, ok := upds[i].(*tg.UpdateEditChannelMessage)
	require.True(t, ok)
	m, ok := edit.Message.(*tg.Message)
	require.True(t, ok)
	return m
}

// The reply to messages.sendReaction in a supergroup carried a hidden edit with
// an empty set next to the reactions update with ours in it; the edit landed
// second and wiped the reaction (#248). Within one envelope the reactions
// update decides, so the edit reaches the pipeline without a reactions field.
func TestOutboxHook_EditYieldsReactionsToTheUpdateBesideIt(t *testing.T) {
	u := &tg.Updates{Updates: []tg.UpdateClass{
		editWithReactions(7, 4118, 4674, tg.MessageReactions{Results: []tg.ReactionCount{}}),
		reactionsUpdate(7, 4118, ourOK),
	}}

	got, logs := passHook(t, u)

	m := editedMessage(t, got, 0)
	_, has := m.GetReactions()
	assert.False(t, has, "the edit's reaction set must yield to the reactions update")
	msg, ok := convertMessage(m, 7)
	require.True(t, ok)
	assert.Nil(t, msg.Reactions, "a stripped edit reads as one that did not carry the field")

	entries := logs.FilterMessage("reaction: edit yields").All()
	require.Len(t, entries, 1)
	assert.Equal(t, map[string]any{
		"chat_id": int64(7),
		"msg_id":  int64(4118),
		"pts":     int64(4674),
		"dropped": "[]",
	}, entries[0].ContextMap())
}

// Where nothing else describes the change the edit's set is all there is: a
// 1:1 reaction, or a large channel's min set, arrives as an edit alone.
func TestOutboxHook_EditAloneKeepsItsReactions(t *testing.T) {
	u := &tg.Updates{Updates: []tg.UpdateClass{
		editWithReactions(7, 4118, 4674, ourOK),
		reactionsUpdate(7, 4119, ourOK),
		reactionsUpdate(8, 4118, ourOK),
	}}

	got, logs := passHook(t, u)

	_, has := editedMessage(t, got, 0).GetReactions()
	assert.True(t, has, "a reactions update about another message, or another chat, is not a pair")
	assert.Empty(t, logs.FilterMessage("reaction: edit yields").All())
}

func TestOutboxHook_PairInACombinedEnvelope(t *testing.T) {
	u := &tg.UpdatesCombined{Updates: []tg.UpdateClass{
		reactionsUpdate(7, 4118, ourOK),
		editWithReactions(7, 4118, 4674, tg.MessageReactions{Min: true, Results: []tg.ReactionCount{}}),
	}}

	got, _ := passHook(t, u)

	edit := got.(*tg.UpdatesCombined).Updates[1].(*tg.UpdateEditChannelMessage)
	_, has := edit.Message.(*tg.Message).GetReactions()
	assert.False(t, has, "order inside the envelope and a min set make no difference")
}
