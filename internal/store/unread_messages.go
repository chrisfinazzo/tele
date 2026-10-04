package store

import "github.com/sorokin-vladimir/tele/internal/domain"

// recomputeUnreadLocked restores the unread invariant for c: the count is the
// last server-authoritative baseline plus the inbound messages observed locally
// above the read pointer. It is the only writer of domain.Chat.UnreadCount inside the
// store, so the value can never drift the way a bare increment could. Caller
// holds the lock.
func (s *SQLiteStore) recomputeUnreadLocked(c *domain.Chat) {
	c.UnreadCount = s.baselineUnread[c.ID] + len(s.unreadMsgs[c.ID])
}

// countBaselineReadLocked reports how many messages of the dialog-list baseline
// a read pointer moving from prev to maxID has covered, and whether that range
// is known at all. Own outgoing messages never counted as unread, and messages
// observed this session are counted by ID on top of the baseline, so both are
// left out. Caller holds the lock.
//
// The range is known only when the loaded tail reaches back to the old pointer.
// A chat never opened this session holds nothing, and a tail that starts above
// prev hides part of the range — counting either would understate what was read.
func (s *SQLiteStore) countBaselineReadLocked(chatID int64, prev, maxID int) (int, bool) {
	msgs := s.messages[domain.HistoryKey{ChatID: chatID}]
	if len(msgs) == 0 || msgs[0].ID > prev {
		return 0, false
	}
	tracked := s.unreadMsgs[chatID]
	n := 0
	for _, m := range msgs {
		if m.ID <= prev || m.ID > maxID || m.IsOut {
			continue
		}
		if _, seen := tracked[m.ID]; seen {
			continue
		}
		n++
	}
	return n, true
}

// ApplyUnreadMessage records an inbound message as unread for its chat and
// returns whether the chat's unread count changed. It is idempotent per message
// ID, so a replayed or duplicated update cannot inflate the count. No-op for an
// unknown chat or for a message at or below the read pointer (already read
// elsewhere and arriving via getDifference catch-up).
//
// The caller decides what counts as inbound; see ApplyIncomingMessage.
func (s *SQLiteStore) ApplyUnreadMessage(chatID int64, msgID int) bool {
	s.mu.Lock()
	c, ok := s.chats[chatID]
	if !ok || msgID <= c.ReadInboxMaxID {
		s.mu.Unlock()
		return false
	}
	tracked := s.unreadMsgs[chatID]
	if _, isTracked := tracked[msgID]; isTracked {
		s.mu.Unlock()
		return false
	}
	if tracked == nil {
		tracked = make(map[int]struct{})
		s.unreadMsgs[chatID] = tracked
	}
	tracked[msgID] = struct{}{}
	s.recomputeUnreadLocked(&c)
	s.chats[chatID] = c
	s.markDirtyLocked(chatID)
	s.mu.Unlock()
	return true
}
