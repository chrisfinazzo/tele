package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

func topicAt(id int, sec int64) domain.Topic {
	return domain.Topic{ChatID: forumID, ID: id, Title: "t", LastMessage: &domain.Message{ID: id * 10, Date: time.Unix(sec, 0)}}
}

func pinned(t domain.Topic) domain.Topic { t.Pinned = true; return t }

func topicIDs(ts []domain.Topic) []int {
	out := make([]int, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

// Pinned topics come first in the order Telegram pinned them, which only the
// first page states; the rest follow their newest message.
func TestTopics_AreListedPinnedFirstThenByActivity(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{
		pinned(topicAt(5, 100)), pinned(topicAt(3, 300)), topicAt(1, 200), topicAt(9, 400),
	})

	assert.Equal(t, []int{5, 3, 9, 1}, topicIDs(s.Topics(forumID)))
}

// A topic read again by id says nothing about where it is pinned, so the order
// the first page stated stands.
func TestTopics_AnUpdateKeepsThePinOrder(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{pinned(topicAt(5, 100)), pinned(topicAt(3, 300))})

	again := pinned(topicAt(5, 500))
	again.Title = "renamed"
	s.UpdateTopics(forumID, []domain.Topic{again})

	got := s.Topics(forumID)
	assert.Equal(t, []int{5, 3}, topicIDs(got))
	assert.Equal(t, "renamed", got[0].Title)
	one, ok := s.Topic(forumID, 5)
	require.True(t, ok)
	assert.Equal(t, "renamed", one.Title)
}

func TestTopics_ADeletedTopicIsGone(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{topicAt(5, 100), topicAt(3, 300)})

	s.RemoveTopics(forumID, []int{3})

	assert.Equal(t, []int{5}, topicIDs(s.Topics(forumID)))
	_, ok := s.Topic(forumID, 3)
	assert.False(t, ok)
}

func incoming(id, topic int) domain.Message {
	return domain.Message{ID: id, ChatID: forumID, TopicID: topic, Date: time.Unix(int64(id), 0)}
}

// A message arriving in a topic is the topic's newest and, unless the account
// wrote it or has read past it, one more unread there. A second delivery of the
// same message counts nothing.
func TestTopics_AnArrivalIsTheTopicsNewsAndItsUnread(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{{ChatID: forumID, ID: 12, Title: "Releases", ReadInboxMaxID: 5, UnreadCount: 1, TopMessageID: 6}})

	msg := incoming(10, 12)
	s.AppendMessage(msg)
	h, known := s.ApplyIncomingTopic(msg)
	assert.Equal(t, domain.HistoryKey{ChatID: forumID, TopicID: 12}, h)
	assert.True(t, known)
	s.ApplyIncomingTopic(msg)

	got, _ := s.Topic(forumID, 12)
	assert.Equal(t, 2, got.UnreadCount, "counted once however often it arrives")
	assert.Equal(t, 10, got.TopMessageID)
	require.NotNil(t, got.LastMessage)
	assert.Equal(t, 10, got.LastMessage.ID)

	own := incoming(11, 12)
	own.IsOut = true
	s.ApplyIncomingTopic(own)
	late := incoming(4, 12)
	s.ApplyIncomingTopic(late)

	got, _ = s.Topic(forumID, 12)
	assert.Equal(t, 2, got.UnreadCount, "our own message and one already read count nothing")
	assert.Equal(t, 11, got.TopMessageID, "a late copy of an old message is not the topic's newest")
}

func TestTopics_AMentionArrivingInATopicIsCountedThere(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{{ChatID: forumID, ID: 12, Title: "Releases", UnreadMentionsCount: 1}})

	msg := incoming(10, 12)
	msg.Mentioned = true
	s.ApplyIncomingTopic(msg)
	s.ApplyIncomingTopic(msg)

	got, _ := s.Topic(forumID, 12)
	assert.Equal(t, 2, got.UnreadMentionsCount)
}

// A message can arrive in a topic nobody has described yet: a new topic, or one
// past the first page. It still counts, under a topic known only by its id.
func TestTopics_AnArrivalInAnUnknownTopicIsHeldUnderItsID(t *testing.T) {
	s := forumStore(t)

	h, known := s.ApplyIncomingTopic(incoming(10, 30))

	assert.False(t, known)
	assert.Equal(t, 30, h.TopicID)
	got, ok := s.Topic(forumID, 30)
	require.True(t, ok)
	assert.Empty(t, got.Title)
	assert.Equal(t, 1, got.UnreadCount)
}

func TestTopics_AnArrivalNamingNoTopicIsGenerals(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{{ChatID: forumID, ID: domain.GeneralTopicID, Title: "General"}})

	h, known := s.ApplyIncomingTopic(incoming(10, 0))

	assert.True(t, known)
	assert.Equal(t, domain.GeneralTopicID, h.TopicID)
	got, _ := s.Topic(forumID, domain.GeneralTopicID)
	assert.Equal(t, 1, got.UnreadCount)
}

// What Telegram says about a topic is the count; arrivals counted before it
// are inside that number already.
func TestTopics_ReadingATopicAgainRebasesItsCount(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{{ChatID: forumID, ID: 12, Title: "Releases"}})
	s.ApplyIncomingTopic(incoming(10, 12))

	s.UpdateTopics(forumID, []domain.Topic{{ChatID: forumID, ID: 12, Title: "Releases", UnreadCount: 0, ReadInboxMaxID: 10}})
	s.ApplyIncomingTopic(incoming(10, 12))

	got, _ := s.Topic(forumID, 12)
	assert.Zero(t, got.UnreadCount)
}

func TestTopics_SurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tele.db")
	chat := domain.Chat{ID: forumID, IsForum: true, Peer: domain.Peer{ID: forumID, Type: domain.PeerSuperGroup}}
	s, err := store.NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	s.SetChat(chat)
	s.SetTopicsPage(forumID, []domain.Topic{pinned(topicAt(5, 100)), pinned(topicAt(3, 300)), topicAt(1, 200)})
	require.NoError(t, s.Close())

	s2, err := store.NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	got := s2.Topics(forumID)
	assert.Equal(t, []int{5, 3, 1}, topicIDs(got))
	require.NotNil(t, got[2].LastMessage)
	assert.Equal(t, 10, got[2].LastMessage.ID)
}

// Topics are what a forum is divided into; a group that stops being a forum
// has none, and one that becomes a forum again starts from Telegram's list.
func TestTopics_GoWhenTopicsAreTurnedOff(t *testing.T) {
	s := forumStore(t)
	s.SetTopicsPage(forumID, []domain.Topic{topicAt(5, 100)})

	s.SetChat(domain.Chat{ID: forumID, Peer: domain.Peer{ID: forumID, Type: domain.PeerSuperGroup}})

	assert.Empty(t, s.Topics(forumID))
}
