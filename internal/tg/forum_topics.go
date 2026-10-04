package tg

import (
	"context"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

// GetForumTopics fetches the first page of a forum's topics, newest activity
// first, the order the official clients list them in.
func (c *GotdClient) GetForumTopics(ctx context.Context, peer domain.Peer, limit int) (ForumTopicsPage, error) {
	api, err := c.acquireAPI()
	if err != nil {
		return ForumTopicsPage{}, err
	}
	var page ForumTopicsPage
	err = WithRetry(ctx, func() error {
		res, err := api.MessagesGetForumTopics(ctx, &tg.MessagesGetForumTopicsRequest{
			Peer:  peerToInput(peer),
			Limit: limit,
		})
		if err != nil {
			c.log.Error("MessagesGetForumTopics failed", zap.Int64("peer_id", peer.ID), zap.Error(err))
			return err
		}
		page = parseForumTopics(res, peer.ID)
		return nil
	})
	return page, err
}

// GetForumTopicsByID fetches named topics of a forum: the authoritative state
// of each, read again whenever something about one of them changes.
func (c *GotdClient) GetForumTopicsByID(ctx context.Context, peer domain.Peer, ids []int) (ForumTopicsPage, error) {
	api, err := c.acquireAPI()
	if err != nil {
		return ForumTopicsPage{}, err
	}
	var page ForumTopicsPage
	err = WithRetry(ctx, func() error {
		res, err := api.MessagesGetForumTopicsByID(ctx, &tg.MessagesGetForumTopicsByIDRequest{
			Peer:   peerToInput(peer),
			Topics: ids,
		})
		if err != nil {
			c.log.Error("MessagesGetForumTopicsByID failed", zap.Int64("peer_id", peer.ID), zap.Error(err))
			return err
		}
		page = parseForumTopics(res, peer.ID)
		return nil
	})
	return page, err
}

// topicServiceEvent recognises the service message that opens or edits a
// forum topic. It names the topic to read again: the one the message opened,
// or the one it was posted in.
func topicServiceEvent(raw tg.MessageClass) (store.Event, bool) {
	svc, ok := raw.(*tg.MessageService)
	if !ok {
		return store.Event{}, false
	}
	chatID := peerIDFromPeer(svc.PeerID)
	switch svc.Action.(type) {
	case *tg.MessageActionTopicCreate:
		return store.Event{Kind: store.EventTopicsChanged, ChatID: chatID, MsgIDs: []int{svc.ID}}, true
	case *tg.MessageActionTopicEdit:
		hdr, ok := svc.ReplyTo.(*tg.MessageReplyHeader)
		if !ok || !hdr.ForumTopic {
			return store.Event{}, false
		}
		topic := hdr.ReplyToMsgID
		if hdr.ReplyToTopID != 0 {
			topic = hdr.ReplyToTopID
		}
		return store.Event{Kind: store.EventTopicsChanged, ChatID: chatID, MsgIDs: []int{topic}}, true
	}
	return store.Event{}, false
}

// ForumTopicsPage is what Telegram said about a forum's topics in one answer:
// the topics it described and the ones it reported deleted.
type ForumTopicsPage struct {
	Topics  []domain.Topic
	Deleted []int
}

// parseForumTopics converts a getForumTopics or getForumTopicsByID answer. The
// newest message of each topic comes in the same answer, and is converted the
// way a history page is, so its preview names its sender.
func parseForumTopics(res *tg.MessagesForumTopics, chatID int64) ForumTopicsPage {
	msgs := parseHistory(&tg.MessagesMessages{Messages: res.Messages, Users: res.Users, Chats: res.Chats}, chatID)

	var page ForumTopicsPage
	for _, raw := range res.Topics {
		switch t := raw.(type) {
		case *tg.ForumTopic:
			topic := domain.Topic{
				ChatID:               chatID,
				ID:                   t.ID,
				Title:                t.Title,
				Pinned:               t.Pinned,
				Closed:               t.Closed,
				Hidden:               t.Hidden,
				TopMessageID:         t.TopMessage,
				ReadInboxMaxID:       t.ReadInboxMaxID,
				ReadOutboxMaxID:      t.ReadOutboxMaxID,
				UnreadCount:          t.UnreadCount,
				UnreadMentionsCount:  t.UnreadMentionsCount,
				UnreadReactionsCount: t.UnreadReactionsCount,
			}
			if d, ok := t.Draft.(*tg.DraftMessage); ok {
				topic.Draft = d.Message
			}
			if m, ok := selectMessageByID(msgs, t.TopMessage); ok {
				topic.LastMessage = &m
			}
			page.Topics = append(page.Topics, topic)
		case *tg.ForumTopicDeleted:
			page.Deleted = append(page.Deleted, t.ID)
		}
	}
	return page
}
