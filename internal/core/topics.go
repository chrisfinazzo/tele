package core

import (
	"context"
	"sort"

	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/store"
)

// topicRefresh coalesces the reads of forum topics the owner owes Telegram.
// Everything about a topic except an arriving message is learned by reading
// the topic again rather than by interpreting the signal, and signals come in
// bursts: one read is in flight per forum at a time, and whatever is asked for
// meanwhile goes out together in the next one.
type topicRefresh struct {
	pending map[int64]map[int]struct{}
	// page marks forums whose first page is to be read again, the only answer
	// that states the order topics are pinned in.
	page    map[int64]bool
	running map[int64]bool
}

// topicsChanged answers Telegram saying something about a forum's topics. A
// chat that is not a forum is left alone: a channel's comment threads are read
// with the same updates and are not topics.
func (o *Owner) topicsChanged(evt store.Event) {
	chat, ok := o.state.Store().GetChat(evt.ChatID)
	if !ok || !chat.IsForum {
		return
	}
	if evt.TopicsPage {
		o.queueTopics(evt.ChatID, nil, true)
	}
	if len(evt.MsgIDs) > 0 {
		o.refreshTopics(evt.ChatID, evt.MsgIDs)
	}
}

// refreshTopics reads named topics of a forum again, now or as soon as the read
// already in flight for that forum returns.
func (o *Owner) refreshTopics(chatID int64, ids []int) {
	o.queueTopics(chatID, ids, false)
}

func (o *Owner) queueTopics(chatID int64, ids []int, page bool) {
	o.topicsMu.Lock()
	defer o.topicsMu.Unlock()
	if o.topics.running == nil {
		o.topics = topicRefresh{
			pending: make(map[int64]map[int]struct{}),
			page:    make(map[int64]bool),
			running: make(map[int64]bool),
		}
	}
	if page {
		o.topics.page[chatID] = true
	}
	if len(ids) > 0 {
		want := o.topics.pending[chatID]
		if want == nil {
			want = make(map[int]struct{})
			o.topics.pending[chatID] = want
		}
		for _, id := range ids {
			want[id] = struct{}{}
		}
	}
	if o.topics.running[chatID] {
		return
	}
	o.topics.running[chatID] = true
	go o.runTopicRefresh(o.ctx, chatID)
}

// runTopicRefresh drains a forum's pending topic reads one request at a time.
func (o *Owner) runTopicRefresh(ctx context.Context, chatID int64) {
	for {
		o.topicsMu.Lock()
		page := o.topics.page[chatID]
		want := o.topics.pending[chatID]
		delete(o.topics.page, chatID)
		delete(o.topics.pending, chatID)
		if !page && len(want) == 0 {
			o.topics.running[chatID] = false
			o.topicsMu.Unlock()
			return
		}
		o.topicsMu.Unlock()

		chat, ok := o.state.Store().GetChat(chatID)
		if !ok {
			continue
		}
		if page {
			got, err := o.client.GetForumTopics(ctx, chat.Peer, forumTopicsPage)
			if err != nil {
				o.log.Warn("forum topics page refresh failed", zap.Int64("chat", chatID), zap.Error(err))
			} else {
				o.state.ApplyTopicsPage(chatID, got.Topics, got.Deleted)
			}
		}
		if len(want) == 0 {
			continue
		}
		ids := make([]int, 0, len(want))
		for id := range want {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		got, err := o.client.GetForumTopicsByID(ctx, chat.Peer, ids)
		if err != nil {
			o.log.Warn("forum topics refresh failed", zap.Int64("chat", chatID), zap.Ints("topics", ids), zap.Error(err))
			continue
		}
		o.state.ApplyTopics(chatID, got.Topics, got.Deleted)
	}
}
