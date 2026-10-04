package store

import (
	"database/sql"

	"go.uber.org/zap"
)

// A gap is a range of messages the account received and this store never saw.
// It is recorded rather than inferred: once the update stream resumes, arriving
// messages land on the tail and the chat looks current again while the missed
// range is still missing, so nothing that compares the tail with the server can
// see it afterwards. The record survives a restart for the same reason - the
// evidence is gone by the time anyone could look for it again.
//
// One position per chat, and it is where the repair has to start from: the
// earliest open gap subsumes every later one, because closing forward from
// there passes through them all.

// Gap returns the id of the message a chat's gap opens after, and whether the
// chat has one at all.
func (s *SQLiteStore) Gap(chatID int64) (int, bool) {
	var after int
	err := s.db.QueryRow(`SELECT after_msg_id FROM chat_gap WHERE chat_id = ?`, chatID).Scan(&after)
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		s.log.Error("read chat gap failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return 0, false
	}
	return after, true
}

// MarkGap records that a chat is missing messages newer than afterMsgID. The
// earlier position wins: a chat that loses a second range while the first is
// still open keeps the first, since a repair starting there covers both. A gap
// opens after a message, so a chat holding none records nothing - that is a
// chat nobody has fetched yet rather than one with a hole in it.
//
// Registering a gap can only move the record backwards. Moving it forward is
// what a repair does, and that is AdvanceGap.
func (s *SQLiteStore) MarkGap(chatID int64, afterMsgID int) {
	if afterMsgID <= 0 {
		return
	}
	_, err := s.db.Exec(`INSERT INTO chat_gap (chat_id, after_msg_id) VALUES (?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET after_msg_id = min(after_msg_id, excluded.after_msg_id)`,
		chatID, afterMsgID)
	if err != nil {
		s.log.Error("mark chat gap failed", zap.Int64("chat_id", chatID), zap.Error(err))
	}
}

// AdvanceGap moves an open gap from one position to a later one, which is how a
// repair records the ground it has covered: a page that lands is not fetched
// again, and an interrupted repair resumes where it stopped rather than from
// the beginning.
//
// It moves the record only while it still says what the repair last saw, and
// reports whether it did. That is what keeps a repair from writing over a hole
// that opened underneath it: a channel losing a second range mid-repair records
// an earlier position, and advancing past that blindly would erase the only
// evidence of it. The same condition means a repair cannot resurrect a gap that
// was closed while its page was in flight.
func (s *SQLiteStore) AdvanceGap(chatID int64, from, to int) bool {
	res, err := s.db.Exec(
		`UPDATE chat_gap SET after_msg_id = ? WHERE chat_id = ? AND after_msg_id = ?`,
		to, chatID, from)
	if err != nil {
		s.log.Error("advance chat gap failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		s.log.Error("advance chat gap failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return false
	}
	return n > 0
}

// CloseGap forgets a gap that still stands where the repair left it, and
// reports whether it did. A hole recorded underneath a finishing repair is a
// hole nobody has closed, and it outlives the repair that did not know about
// it.
func (s *SQLiteStore) CloseGap(chatID int64, at int) bool {
	res, err := s.db.Exec(`DELETE FROM chat_gap WHERE chat_id = ? AND after_msg_id = ?`, chatID, at)
	if err != nil {
		s.log.Error("close chat gap failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		s.log.Error("close chat gap failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return false
	}
	return n > 0
}

// ClearGap forgets a chat's gap whatever it says. It is for the tail reload,
// which throws away the history the gap was a hole in, so no position in that
// history means anything afterwards.
func (s *SQLiteStore) ClearGap(chatID int64) {
	if _, err := s.db.Exec(`DELETE FROM chat_gap WHERE chat_id = ?`, chatID); err != nil {
		s.log.Error("clear chat gap failed", zap.Int64("chat_id", chatID), zap.Error(err))
	}
}

// TailMessageID is the id of the newest message the store holds for a chat, or
// zero for a chat it holds none for.
//
// Messages in memory answer it when there are any: they are ordered oldest
// first and a chat nobody opened still collects its arrivals there, so the last
// one is the newest either way. Only a chat this session has not touched at all
// falls through to disk, which is what keeps a gap opening on a quiet chat from
// loading five hundred messages to read one number.
//
// A forum is asked across all its topics, since its gap lies across all of
// them. Memory alone cannot answer for it: a topic nobody opened this session
// is held only on disk, and its newest message may be the forum's newest, so
// the two are compared (#275).
func (s *SQLiteStore) TailMessageID(chatID int64) int {
	s.mu.RLock()
	forum := s.chats[chatID].IsForum
	purging := s.purgePendingLocked(chatID)
	held := 0
	for _, h := range s.chatHistoriesLocked(chatID) {
		if msgs := s.messages[h]; len(msgs) > 0 && msgs[len(msgs)-1].ID > held {
			held = msgs[len(msgs)-1].ID
		}
	}
	s.mu.RUnlock()
	if (held > 0 && !forum) || purging {
		return held
	}
	return max(held, s.diskTailMessageID(chatID))
}

// diskTailMessageID is the newest message id persisted for a chat.
func (s *SQLiteStore) diskTailMessageID(chatID int64) int {
	var newest sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(msg_id) FROM messages WHERE chat_id = ?`, chatID).Scan(&newest); err != nil {
		s.log.Error("read tail message id failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return 0
	}
	return int(newest.Int64)
}
