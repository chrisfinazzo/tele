package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

// forumCmdOwner is a command owner that also holds a forum with a queue.
func forumCmdOwner(t *testing.T, c *stubClient) *Owner {
	t.Helper()
	o, st := newCmdOwner(t, c)
	st.SetChat(forumDialog())
	o.SetOutbox(newOutboxStore(t))
	return o
}

func TestSend_IntoATopicLandsInThatTopic(t *testing.T) {
	c := &stubClient{sentID: 90}
	o := forumCmdOwner(t, c)
	ctx := runWorker(t, o)

	require.NoError(t, o.Send(ctx, SendRequest{Ref: "r1", ChatID: forumChat, TopicID: 12, Text: "hi"}))

	waitFor(t, "the message never landed in its topic", func() bool {
		return len(o.state.Store().Messages(domain.HistoryKey{ChatID: forumChat, TopicID: 12})) == 1
	})
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	assert.Equal(t, 12, c.sentTopic)
}

// A queued send is shown in the history it was composed in and nowhere else:
// another topic of the same forum is another conversation.
func TestSend_IsShownOnlyInItsOwnTopic(t *testing.T) {
	o := forumCmdOwner(t, &stubClient{})

	require.NoError(t, o.Send(context.Background(), SendRequest{Ref: "r1", ChatID: forumChat, TopicID: 12, Text: "hi"}))

	in := project.BuildHistory(o.reader(), project.HistoryWindow{ChatID: forumChat, TopicID: 12, Before: 10})
	other := project.BuildHistory(o.reader(), project.HistoryWindow{ChatID: forumChat, TopicID: 30, Before: 10})
	require.Len(t, in.Outbox, 1)
	assert.Equal(t, 12, in.Outbox[0].TopicID)
	assert.Empty(t, other.Outbox)
}

func TestSendMedia_IntoATopicNamesIt(t *testing.T) {
	c := &stubClient{}
	o := forumCmdOwner(t, c)
	ctx := runWorker(t, o)
	one := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(one, []byte("x"), 0o600))

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{Ref: "m1", ChatID: forumChat, TopicID: 12, Files: []MediaFile{{Path: one}}}))

	waitFor(t, "the file was never sent", func() bool {
		c.sendMu.Lock()
		defer c.sendMu.Unlock()
		return c.sentMediaN == 1
	})
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	assert.Equal(t, 12, c.mediaTopic)
}

func TestSendMedia_AnAlbumIntoATopicNamesIt(t *testing.T) {
	c := &stubClient{}
	o := forumCmdOwner(t, c)
	ctx := runWorker(t, o)
	files := []MediaFile{{Path: writeJPEG(t, "a.jpg")}, {Path: writeJPEG(t, "b.jpg")}}

	require.NoError(t, o.SendMedia(ctx, MediaSendRequest{Ref: "m1", ChatID: forumChat, TopicID: 12, Files: files}))

	waitFor(t, "the album was never sent", func() bool {
		c.sendMu.Lock()
		defer c.sendMu.Unlock()
		return c.albumItems == 2
	})
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	assert.Equal(t, 12, c.albumTopic)
}

// A draft is kept where the message would go: in a forum, in its topic.
func TestSaveDraft_InATopicIsTheTopics(t *testing.T) {
	c := &stubClient{}
	o := forumCmdOwner(t, c)
	o.state.Store().SetTopicsPage(forumChat, []domain.Topic{{ChatID: forumChat, ID: 12, Title: "Releases"}})

	require.NoError(t, o.SaveDraft(context.Background(), domain.HistoryKey{ChatID: forumChat, TopicID: 12}, "half"))

	got, _ := o.state.Store().Topic(forumChat, 12)
	assert.Equal(t, "half", got.Draft)
	chat, _ := o.state.Store().GetChat(forumChat)
	assert.Empty(t, chat.Draft, "the forum itself has no draft")
	assert.Equal(t, 12, c.draftTopic)
}

func TestADraftFromAnotherDeviceLandsInItsTopic(t *testing.T) {
	o := forumCmdOwner(t, &stubClient{})
	o.state.Store().SetTopicsPage(forumChat, []domain.Topic{{ChatID: forumChat, ID: 12, Title: "Releases"}})

	o.handleEvent(store.Event{Kind: store.EventDraftMessage, ChatID: forumChat, TopicID: 12, Draft: "half"})

	got, _ := o.state.Store().Topic(forumChat, 12)
	assert.Equal(t, "half", got.Draft)
}

func TestSetTyping_InATopicNamesIt(t *testing.T) {
	c := &stubClient{}
	o := forumCmdOwner(t, c)

	require.NoError(t, o.SetTyping(context.Background(), domain.HistoryKey{ChatID: forumChat, TopicID: 12}, domain.TypingActionTyping))

	assert.Equal(t, 12, c.typingTopic)
}

// Somebody typing in one topic is not typing in the others, so the client is
// told which.
func TestTypingInATopicIsReportedWithIt(t *testing.T) {
	o := forumCmdOwner(t, &stubClient{})

	o.handleEvent(store.Event{Kind: store.EventTyping, ChatID: forumChat, TopicID: 12, TypingAction: domain.TypingActionTyping})

	select {
	case got := <-o.Typing():
		assert.Equal(t, int64(forumChat), got.ChatID)
		assert.Equal(t, 12, got.TopicID)
	default:
		t.Fatal("no typing reported")
	}
}

// A forward into a forum goes into a topic, and the comment that precedes it
// goes into the same one.
func TestForward_IntoATopicTakesItsCommentAlong(t *testing.T) {
	c := &stubClient{}
	o := forumCmdOwner(t, c)

	require.NoError(t, o.Forward(context.Background(), 1, domain.HistoryKey{ChatID: forumChat, TopicID: 12}, []int{5}, "look"))

	assert.Equal(t, 12, c.forwardedTopic)
	queued := o.outbox.ForChat(forumChat)
	require.Len(t, queued, 1)
	assert.Equal(t, 12, queued[0].TopicID)
}
