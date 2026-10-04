package store

import (
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/domain"
)

// The reaction trace (#248): every write that changes a held message's reaction
// set says so, with the write that did it. A reaction that vanishes without an
// error was overwritten by one of these, and without the line there is no
// telling a hidden edit from a history page. Debug only, i.e. under -e.

func (s *SQLiteStore) traceReactions() bool {
	return s.log.Core().Enabled(zap.DebugLevel)
}

// traceReactionChangeLocked logs one message's set going from was to now, if it
// moved. Caller holds the lock.
func (s *SQLiteStore) traceReactionChangeLocked(chatID int64, msgID int, via string, was, now []domain.Reaction) {
	if !s.traceReactions() || domain.SameReactions(was, now) {
		return
	}
	s.log.Debug("reaction: store write",
		zap.Int64("chat_id", chatID),
		zap.Int("msg_id", msgID),
		zap.String("via", via),
		zap.String("was", domain.FormatReactions(was)),
		zap.String("now", domain.FormatReactions(now)),
	)
}

// heldReactionsLocked snapshots the reaction sets of a chat's held messages
// before a write that replaces them wholesale, or nil when tracing is off.
// Caller holds the lock.
func (s *SQLiteStore) heldReactionsLocked(chatID int64) map[int][]domain.Reaction {
	if !s.traceReactions() {
		return nil
	}
	held := make(map[int][]domain.Reaction)
	for _, h := range s.chatHistoriesLocked(chatID) {
		for _, m := range s.messages[h] {
			held[m.ID] = m.Reactions
		}
	}
	return held
}

// traceHeldReactionsLocked reports every message that was held before a
// wholesale write, is still held after it, and came out with a different set.
// Caller holds the lock.
func (s *SQLiteStore) traceHeldReactionsLocked(chatID int64, via string, held map[int][]domain.Reaction) {
	if held == nil {
		return
	}
	for _, h := range s.chatHistoriesLocked(chatID) {
		for _, m := range s.messages[h] {
			if was, ok := held[m.ID]; ok {
				s.traceReactionChangeLocked(chatID, m.ID, via, was, m.Reactions)
			}
		}
	}
}
