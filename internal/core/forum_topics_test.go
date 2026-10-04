package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/state"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
)

const forumChat = 50

// topicConn is a connection that also answers for forum topics. It records
// what was asked so a test can tell a fetch from no fetch.
type topicConn struct {
	*stubConn
	mu    sync.Mutex
	pages map[int64]internaltg.ForumTopicsPage
	byID  map[int64]internaltg.ForumTopicsPage
	asked []int64
	byIDs [][]int
	gate  chan struct{}
}

func newTopicConn() *topicConn {
	return &topicConn{
		stubConn: &stubConn{},
		pages:    make(map[int64]internaltg.ForumTopicsPage),
		byID:     make(map[int64]internaltg.ForumTopicsPage),
	}
}

func (c *topicConn) GetArchivedDialogs(context.Context) ([]domain.Chat, error) { return nil, nil }

func (c *topicConn) GetForumTopics(_ context.Context, peer domain.Peer, _ int) (internaltg.ForumTopicsPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, peer.ID)
	return c.pages[peer.ID], nil
}

func (c *topicConn) GetForumTopicsByID(_ context.Context, peer domain.Peer, ids []int) (internaltg.ForumTopicsPage, error) {
	if c.gate != nil {
		<-c.gate
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byIDs = append(c.byIDs, append([]int(nil), ids...))
	return c.byID[peer.ID], nil
}

func (c *topicConn) askedFor() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.asked...)
}

func (c *topicConn) byIDCalls() [][]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]int(nil), c.byIDs...)
}

func forumDialog() domain.Chat {
	return domain.Chat{ID: forumChat, Title: "Dev", IsForum: true, Peer: domain.Peer{ID: forumChat, Type: domain.PeerSuperGroup}}
}

func forumOwner(t *testing.T, c *topicConn) (*Owner, *state.State) {
	t.Helper()
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(forumDialog())
	return o, s
}

func arrival(id, topic int) store.Event {
	return store.Event{Kind: store.EventNewMessage, Message: domain.Message{
		ID: id, ChatID: forumChat, TopicID: topic, Date: time.Unix(int64(id), 0), Text: "hi",
	}}
}

func TestOwner_AnArrivalInAForumLandsInItsTopic(t *testing.T) {
	c := newTopicConn()
	o, s := forumOwner(t, c)
	s.Store().SetTopicsPage(forumChat, []domain.Topic{{ChatID: forumChat, ID: 12, Title: "Releases"}})

	o.handleEvent(arrival(40, 12))

	got, _ := s.Store().Topic(forumChat, 12)
	assert.Equal(t, 1, got.UnreadCount)
	assert.Equal(t, 40, got.TopMessageID)
	assert.Len(t, s.Store().Messages(domain.HistoryKey{ChatID: forumChat, TopicID: 12}), 1)
	assert.Empty(t, c.byIDCalls(), "a topic already described is not asked about")
}

// A message for a topic nobody has described is the cue to ask Telegram what
// that topic is; until the answer comes it is a topic known by its id alone.
func TestOwner_AnArrivalInAnUnknownTopicAsksWhatItIs(t *testing.T) {
	c := newTopicConn()
	c.byID[forumChat] = internaltg.ForumTopicsPage{Topics: []domain.Topic{{ChatID: forumChat, ID: 30, Title: "Design"}}}
	o, s := forumOwner(t, c)

	o.handleEvent(arrival(40, 30))

	require.Eventually(t, func() bool {
		got, _ := s.Store().Topic(forumChat, 30)
		return got.Title == "Design"
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, [][]int{{30}}, c.byIDCalls())
}

func TestOwner_ATopicSignalReadsTheTopicAgain(t *testing.T) {
	c := newTopicConn()
	c.byID[forumChat] = internaltg.ForumTopicsPage{Topics: []domain.Topic{{ChatID: forumChat, ID: 12, Title: "Releases", ReadInboxMaxID: 90}}}
	o, s := forumOwner(t, c)

	o.handleEvent(store.Event{Kind: store.EventTopicsChanged, ChatID: forumChat, MsgIDs: []int{12}})

	require.Eventually(t, func() bool {
		got, _ := s.Store().Topic(forumChat, 12)
		return got.ReadInboxMaxID == 90
	}, time.Second, 5*time.Millisecond)
}

// A channel's comment threads are read the same way a topic is, and they are
// not topics: a chat that is not a forum has nothing to read again.
func TestOwner_ATopicSignalForAChatThatIsNoForumIsIgnored(t *testing.T) {
	c := newTopicConn()
	o, s := newOwnerWithClient(t, c)
	s.Store().SetChat(domain.Chat{ID: 60, Peer: domain.Peer{ID: 60, Type: domain.PeerSuperGroup}})

	o.handleEvent(store.Event{Kind: store.EventTopicsChanged, ChatID: 60, MsgIDs: []int{12}})
	o.handleEvent(store.Event{Kind: store.EventTopicsChanged, ChatID: 60, TopicsPage: true})

	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, c.byIDCalls())
	assert.Empty(t, c.askedFor())
}

func TestOwner_APinChangeReadsTheFirstPageAgain(t *testing.T) {
	c := newTopicConn()
	c.pages[forumChat] = internaltg.ForumTopicsPage{Topics: []domain.Topic{{ChatID: forumChat, ID: 12, Title: "Releases", Pinned: true}}}
	o, s := forumOwner(t, c)

	o.handleEvent(store.Event{Kind: store.EventTopicsChanged, ChatID: forumChat, TopicsPage: true})

	require.Eventually(t, func() bool {
		got, _ := s.Store().Topic(forumChat, 12)
		return got.Pinned
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, []int64{forumChat}, c.askedFor())
}

// Topic signals come in bursts. One read is in flight per forum, and whatever
// is asked for meanwhile goes out together in the next.
func TestOwner_TopicReadsAskedForMeanwhileGoOutTogether(t *testing.T) {
	c := newTopicConn()
	c.gate = make(chan struct{})
	o, _ := forumOwner(t, c)

	o.handleEvent(store.Event{Kind: store.EventTopicsChanged, ChatID: forumChat, MsgIDs: []int{30}})
	time.Sleep(20 * time.Millisecond)
	o.handleEvent(store.Event{Kind: store.EventTopicsChanged, ChatID: forumChat, MsgIDs: []int{32}})
	o.handleEvent(store.Event{Kind: store.EventTopicsChanged, ChatID: forumChat, MsgIDs: []int{31}})
	close(c.gate)

	require.Eventually(t, func() bool { return len(c.byIDCalls()) == 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, [][]int{{30}, {31, 32}}, c.byIDCalls())
}

func TestOwner_BootstrapLoadsEachForumsTopics(t *testing.T) {
	c := newTopicConn()
	c.dialogs = []domain.Chat{
		forumDialog(),
		{ID: 60, Title: "Plain", Peer: domain.Peer{ID: 60, Type: domain.PeerSuperGroup}},
	}
	c.pages[forumChat] = internaltg.ForumTopicsPage{Topics: []domain.Topic{
		{ChatID: forumChat, ID: 12, Title: "Releases"},
		{ChatID: forumChat, ID: 1, Title: "General"},
	}}
	o, s := newOwnerWithClient(t, c)

	require.NoError(t, o.Bootstrap(context.Background()))

	require.Eventually(t, func() bool { return len(s.Store().Topics(forumChat)) == 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []int64{forumChat}, c.askedFor(), "an ordinary group has no topics to ask about")
}
