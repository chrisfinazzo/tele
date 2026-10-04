package tg

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func replyTo(t *testing.T, r tg.InputReplyToClass) *tg.InputReplyToMessage {
	t.Helper()
	m, ok := r.(*tg.InputReplyToMessage)
	require.True(t, ok, "a send into a topic carries a reply header")
	return m
}

// Every kind of send lands in the topic it was composed in.
func TestSendsIntoATopicNameIt(t *testing.T) {
	peer := &tg.InputPeerChannel{ChannelID: 50}

	text := buildSendRequest(peer, "hi", 7, 0, 12, nil)
	assert.Equal(t, 12, replyTo(t, text.ReplyTo).ReplyToMsgID)

	media := buildSendMediaRequest(peer, &tg.InputMediaEmpty{}, "", 7, 40, 12, nil)
	assert.Equal(t, 40, replyTo(t, media.ReplyTo).ReplyToMsgID)
	assert.Equal(t, 12, replyTo(t, media.ReplyTo).TopMsgID)

	album := buildSendMultiMediaRequest(peer, []AlbumItem{{Media: &tg.InputMediaEmpty{}}}, []int64{7}, 0, 12)
	assert.Equal(t, 12, replyTo(t, album.ReplyTo).ReplyToMsgID)
}

// A draft and a typing indicator belong to the topic they were made in, and
// General's belong to the forum as an ordinary chat's would.
func TestADraftAndTypingNameTheirTopic(t *testing.T) {
	peer := &tg.InputPeerChannel{ChannelID: 50}

	draft := buildSaveDraftRequest(peer, "half", 12)
	assert.Equal(t, 12, replyTo(t, draft.ReplyTo).ReplyToMsgID)
	assert.Nil(t, buildSaveDraftRequest(peer, "half", 1).ReplyTo)

	assert.Equal(t, 12, buildSetTypingRequest(peer, &tg.SendMessageTypingAction{}, 12).TopMsgID)
	assert.Zero(t, buildSetTypingRequest(peer, &tg.SendMessageTypingAction{}, 1).TopMsgID)
}

// A forward into a topic names the topic as its top; into General it names
// nothing, since that is where a message naming no topic lands.
func TestAForwardIntoATopicNamesIt(t *testing.T) {
	from, to := &tg.InputPeerChannel{ChannelID: 60}, &tg.InputPeerChannel{ChannelID: 50}

	assert.Equal(t, 12, buildForwardRequest(from, to, []int{1}, []int64{7}, 12).TopMsgID)
	assert.Zero(t, buildForwardRequest(from, to, []int{1}, []int64{7}, 1).TopMsgID)
	assert.Zero(t, buildForwardRequest(from, to, []int{1}, []int64{7}, 0).TopMsgID)
}
