package tg

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseForumTopics(t *testing.T) {
	res := &tg.MessagesForumTopics{
		Count: 140,
		Topics: []tg.ForumTopicClass{
			&tg.ForumTopic{
				ID: 12, Title: "Releases", Pinned: true, TopMessage: 40,
				ReadInboxMaxID: 38, ReadOutboxMaxID: 39,
				UnreadCount: 2, UnreadMentionsCount: 1, UnreadReactionsCount: 3,
				Draft: &tg.DraftMessage{Message: "half a thought"},
			},
			&tg.ForumTopic{ID: 1, Title: "General", Closed: true, Hidden: true, TopMessage: 41},
			&tg.ForumTopicDeleted{ID: 30},
		},
		Messages: []tg.MessageClass{
			&tg.Message{ID: 40, Date: 1700000000, Message: "v1.12 is out", FromID: &tg.PeerUser{UserID: 7},
				ReplyTo: &tg.MessageReplyHeader{ForumTopic: true, ReplyToMsgID: 12}},
			&tg.Message{ID: 41, Date: 1700000100, Message: "hello"},
		},
		Users: []tg.UserClass{&tg.User{ID: 7, FirstName: "Alice"}},
	}

	page := parseForumTopics(res, 50)

	require.Len(t, page.Topics, 2)
	rel := page.Topics[0]
	assert.Equal(t, int64(50), rel.ChatID)
	assert.Equal(t, 12, rel.ID)
	assert.Equal(t, "Releases", rel.Title)
	assert.True(t, rel.Pinned)
	assert.Equal(t, 40, rel.TopMessageID)
	assert.Equal(t, 38, rel.ReadInboxMaxID)
	assert.Equal(t, 39, rel.ReadOutboxMaxID)
	assert.Equal(t, 2, rel.UnreadCount)
	assert.Equal(t, 1, rel.UnreadMentionsCount)
	assert.Equal(t, 3, rel.UnreadReactionsCount)
	assert.Equal(t, "half a thought", rel.Draft)
	require.NotNil(t, rel.LastMessage)
	assert.Equal(t, "v1.12 is out", rel.LastMessage.Text)
	assert.Equal(t, "Alice", rel.LastMessage.SenderName)
	assert.Equal(t, 12, rel.LastMessage.TopicID)

	gen := page.Topics[1]
	assert.True(t, gen.Closed)
	assert.True(t, gen.Hidden)
	require.NotNil(t, gen.LastMessage)
	assert.Equal(t, "hello", gen.LastMessage.Text)

	assert.Equal(t, []int{30}, page.Deleted)
	assert.Equal(t, 140, page.Total, "how many topics the forum has, beyond this page")
}
