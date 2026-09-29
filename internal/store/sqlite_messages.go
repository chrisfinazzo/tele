package store

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"go.uber.org/zap"
)

func (s *SQLiteStore) Messages(chatID int64) []domain.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.messages[chatID]
	if msgs == nil {
		return nil
	}
	cp := make([]domain.Message, len(msgs))
	copy(cp, msgs)
	return cp
}

func (s *SQLiteStore) SetMessages(chatID int64, msgs []domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setMessagesLocked(chatID, msgs, len(msgs), "SetMessages")
}

// MergeMessages merges a fetched page into a chat's stored history and reports
// how many messages it added. Reading the held history, merging and storing the
// result happen under one hold of the lock, which is what keeps a message
// arriving mid-fetch from being written back out of existence: read and write
// as two calls leave a window where the arrival lands between them and is lost
// when the merged page replaces the slice.
//
// A page that adds nothing still lands, because a message can come back edited
// without changing the count. The caller decides what to do about a zero, and
// what it is told is what the count changed by, not whether anything did.
func (s *SQLiteStore) MergeMessages(chatID int64, msgs []domain.Message) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.messages[chatID]
	merged := domain.MergeMessages(held, msgs)
	s.setMessagesLocked(chatID, merged, len(merged), "MergeMessages")
	return len(merged) - len(held)
}

// RepairMessages merges a page in the way MergeMessages does, and leaves the
// chat no deeper than it found it: the cap applies as if the page had never
// come, trimming the oldest to make room.
//
// The floor exists to protect a scrollback somebody scrolled to, so that an
// arriving message cannot cap it back down. A page that closes a gap was asked
// for by nobody, and letting it raise the floor would turn a repair into a
// permanent rise in what the chat costs to hold, once per hole.
func (s *SQLiteStore) RepairMessages(chatID int64, msgs []domain.Message) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.messages[chatID]
	merged := domain.MergeMessages(held, msgs)
	s.setMessagesLocked(chatID, merged, s.msgFloor[chatID], "RepairMessages")
	// What the page brought, counted before the cap trims the other end: a
	// repair that adds fifty and pushes fifty older ones out still changed
	// every window looking at it.
	return len(merged) - len(held)
}

// setMessagesLocked replaces a chat's history with msgs, taking a copy: the
// slice handed in may be the caller's own, or the store's own held slice come
// back through a merge. floor is how deep this write claims the chat was
// filled, which is what the cap will not trim below. via names the write for
// the reaction trace. Caller holds the lock.
func (s *SQLiteStore) setMessagesLocked(chatID int64, msgs []domain.Message, floor int, via string) {
	cp := make([]domain.Message, len(msgs))
	copy(cp, msgs)
	held := s.heldReactionsLocked(chatID)
	defer s.traceHeldReactionsLocked(chatID, via, held)

	newIDs := make(map[int]struct{}, len(cp))
	for _, m := range cp {
		newIDs[m.ID] = struct{}{}
	}
	// Re-index this chat: drop entries for the replaced messages and mark rows the
	// new set no longer contains for deletion on disk.
	for _, m := range s.messages[chatID] {
		delete(s.msgChat, m.ID)
		if _, keep := newIDs[m.ID]; !keep {
			s.markMsgDeletedLocked(chatID, m.ID)
		}
	}
	s.messages[chatID] = cp
	// Remember how deep this chat was filled, so an arriving message does not
	// cap the scrollback back down (see capMessagesLocked).
	if s.msgFloor == nil {
		s.msgFloor = make(map[int64]int)
	}
	s.msgFloor[chatID] = floor
	s.capMessagesLocked(chatID)
	if chat, ok := s.chats[chatID]; ok && sharedPtsBox(chat.Peer) {
		for _, m := range s.messages[chatID] {
			s.msgChat[m.ID] = chatID
		}
	}
	for _, m := range s.messages[chatID] {
		s.markMsgDirtyLocked(chatID, m.ID)
	}
}

// markMsgDirtyLocked queues an upsert of (chatID, msgID) for the next flush.
// Optimistic sentinel messages (negative ids) are session-only and never
// persisted. Caller holds the lock.
func (s *SQLiteStore) markMsgDirtyLocked(chatID int64, msgID int) {
	if msgID <= 0 {
		return
	}
	if d := s.deletedMsgs[chatID]; d != nil {
		delete(d, msgID)
	}
	m := s.dirtyMsgs[chatID]
	if m == nil {
		m = make(map[int]struct{})
		s.dirtyMsgs[chatID] = m
	}
	m[msgID] = struct{}{}
}

// markMsgDeletedLocked queues a delete of (chatID, msgID) for the next flush.
// Caller holds the lock.
func (s *SQLiteStore) markMsgDeletedLocked(chatID int64, msgID int) {
	if msgID <= 0 {
		return
	}
	if d := s.dirtyMsgs[chatID]; d != nil {
		delete(d, msgID)
	}
	m := s.deletedMsgs[chatID]
	if m == nil {
		m = make(map[int]struct{})
		s.deletedMsgs[chatID] = m
	}
	m[msgID] = struct{}{}
}

type msgUpsert struct {
	chatID int64
	msgID  int
	date   int64
	data   []byte
}

type msgDelete struct {
	chatID int64
	msgID  int
}

// snapshotMessageWritesLocked drains the dirty/deleted message sets into flat
// slices, reading upsert payloads from the current in-memory messages. Caller
// holds the lock.
func (s *SQLiteStore) snapshotMessageWritesLocked() ([]msgUpsert, []msgDelete) {
	var upserts []msgUpsert
	for chatID, ids := range s.dirtyMsgs {
		for _, m := range s.messages[chatID] {
			if _, ok := ids[m.ID]; !ok {
				continue
			}
			b, err := json.Marshal(m)
			if err != nil {
				s.log.Error("marshal message failed", zap.Int64("chat_id", chatID), zap.Int("msg_id", m.ID), zap.Error(err))
				continue
			}
			upserts = append(upserts, msgUpsert{chatID: chatID, msgID: m.ID, date: m.Date.Unix(), data: b})
		}
	}
	s.dirtyMsgs = make(map[int64]map[int]struct{})

	var deletes []msgDelete
	for chatID, ids := range s.deletedMsgs {
		inFlight := s.deletingMsgs[chatID]
		if inFlight == nil {
			inFlight = make(map[int]struct{}, len(ids))
			s.deletingMsgs[chatID] = inFlight
		}
		for msgID := range ids {
			deletes = append(deletes, msgDelete{chatID: chatID, msgID: msgID})
			inFlight[msgID] = struct{}{}
		}
	}
	s.deletedMsgs = make(map[int64]map[int]struct{})
	return upserts, deletes
}

// clearDeletesInFlight forgets deletes whose flush transaction has finished.
func (s *SQLiteStore) clearDeletesInFlight(deletes []msgDelete) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range deletes {
		ids := s.deletingMsgs[d.chatID]
		delete(ids, d.msgID)
		if len(ids) == 0 {
			delete(s.deletingMsgs, d.chatID)
		}
	}
}

// msgDeletePendingLocked reports whether a message is queued for deletion or has
// a delete in flight, so a read from disk must skip its row. Caller holds the lock.
func (s *SQLiteStore) msgDeletePendingLocked(chatID int64, msgID int) bool {
	if _, ok := s.deletedMsgs[chatID][msgID]; ok {
		return true
	}
	_, ok := s.deletingMsgs[chatID][msgID]
	return ok
}

// flushMessageRows applies queued upserts and deletes in one transaction. Runs
// off-lock. Logs errors; the Store interface does not propagate them.
func (s *SQLiteStore) flushMessageRows(upserts []msgUpsert, deletes []msgDelete) {
	if len(upserts) == 0 && len(deletes) == 0 {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		s.log.Error("begin message flush failed", zap.Error(err))
		return
	}
	for _, u := range upserts {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO messages(chat_id, msg_id, date, data) VALUES (?, ?, ?, ?)`,
			u.chatID, u.msgID, u.date, u.data); err != nil {
			_ = tx.Rollback()
			s.log.Error("upsert message failed", zap.Int64("chat_id", u.chatID), zap.Int("msg_id", u.msgID), zap.Error(err))
			return
		}
	}
	for _, d := range deletes {
		if _, err := tx.Exec(`DELETE FROM messages WHERE chat_id = ? AND msg_id = ?`, d.chatID, d.msgID); err != nil {
			_ = tx.Rollback()
			s.log.Error("delete message failed", zap.Int64("chat_id", d.chatID), zap.Int("msg_id", d.msgID), zap.Error(err))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.log.Error("commit message flush failed", zap.Error(err))
	}
}

// mergeMessagesByID unions a chat's on-disk tail (disk) with its in-memory
// messages (mem), keeping the in-memory copy on an id collision (live updates
// are fresher) and returning the result sorted by (date, id).
func mergeMessagesByID(disk, mem []domain.Message) []domain.Message {
	seen := make(map[int]struct{}, len(mem))
	for _, m := range mem {
		seen[m.ID] = struct{}{}
	}
	out := make([]domain.Message, 0, len(disk)+len(mem))
	for _, d := range disk {
		if _, ok := seen[d.ID]; !ok {
			out = append(out, d)
		}
	}
	out = append(out, mem...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.Before(out[j].Date)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// LoadMessages loads a chat's persisted message tail into memory on first open,
// merging it with any messages already held (live updates). Idempotent per chat;
// a pure read that never queues a write. See issue #139.
func (s *SQLiteStore) LoadMessages(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded[chatID] {
		return
	}
	s.loaded[chatID] = true

	rows, err := s.db.Query(`SELECT data FROM messages WHERE chat_id = ? ORDER BY date, msg_id`, chatID)
	if err != nil {
		s.log.Error("load messages failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return
	}
	defer func() { _ = rows.Close() }()

	var disk []domain.Message
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			s.log.Error("scan message failed", zap.Int64("chat_id", chatID), zap.Error(err))
			return
		}
		var m domain.Message
		if err := json.Unmarshal(data, &m); err != nil {
			s.log.Error("unmarshal message failed", zap.Int64("chat_id", chatID), zap.Error(err))
			continue
		}
		// A delete that has not reached disk yet already happened as far as the
		// rest of the app is concerned; its row must not come back on open.
		if s.msgDeletePendingLocked(chatID, m.ID) {
			continue
		}
		disk = append(disk, m)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("iterate messages failed", zap.Int64("chat_id", chatID), zap.Error(err))
		return
	}

	if mem := s.messages[chatID]; len(mem) == 0 {
		s.messages[chatID] = disk
	} else {
		s.messages[chatID] = mergeMessagesByID(disk, mem)
	}
	s.capMessagesLocked(chatID)
	if chat, ok := s.chats[chatID]; ok && sharedPtsBox(chat.Peer) {
		for _, m := range s.messages[chatID] {
			s.msgChat[m.ID] = chatID
		}
	}
}

// capMessagesLocked trims a chat's message slice to the newest
// MaxMessagesPerChat, dropping the oldest from the front and clearing their
// index entries. Caller holds the lock. See issue #73.
//
// The cap bounds what arriving messages accumulate; it never trims below what
// was last set outright. Scrolling far into history sets a longer tail on
// purpose, and one incoming message must not throw that scrollback away — the
// view is built from what the store holds, so the trim would be visible.
func (s *SQLiteStore) capMessagesLocked(chatID int64) {
	msgs := s.messages[chatID]
	limit := MaxMessagesPerChat
	if floor := s.msgFloor[chatID]; floor > limit {
		limit = floor
	}
	if len(msgs) <= limit {
		return
	}
	drop := len(msgs) - limit
	for _, m := range msgs[:drop] {
		delete(s.msgChat, m.ID)
		s.markMsgDeletedLocked(chatID, m.ID)
	}
	s.messages[chatID] = msgs[drop:]
}

// BumpChatLastMessage updates a chat's last-message preview and moves it up in
// the list, WITHOUT appending to the chat's message slice. It optimistically
// surfaces a chat that just received an outgoing message sent from elsewhere
// (e.g. a forward target), whose full message arrives later via the update
// stream (or on next open). No-op if the chat is unknown.
func (s *SQLiteStore) BumpChatLastMessage(chatID int64, msg domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat, ok := s.chats[chatID]
	if !ok {
		return
	}
	m := msg
	chat.LastMessage = &m
	s.chats[chatID] = chat
	s.orderDirty = true // newer last-message moves the chat in the list
	s.markDirtyLocked(chatID)
}

// AppendMessage adds a message to a chat, or replaces it when that ID is already
// held. The same message legitimately arrives twice — once in the reply to the
// RPC that created it, once from a later getDifference — and the newer copy may
// carry more (a resolved sender name, filled-in media refs), so it wins in place
// rather than appearing a second time. Sentinel IDs are negative and unique, so
// optimistic messages never collide here.
func (s *SQLiteStore) AppendMessage(msg domain.Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.ID > 0 {
		for i := range s.messages[msg.ChatID] {
			if s.messages[msg.ChatID][i].ID == msg.ID {
				s.traceReactionChangeLocked(msg.ChatID, msg.ID, "AppendMessage", s.messages[msg.ChatID][i].Reactions, msg.Reactions)
				s.messages[msg.ChatID][i] = msg
				s.markMsgDirtyLocked(msg.ChatID, msg.ID)
				return false
			}
		}
	}
	s.messages[msg.ChatID] = append(s.messages[msg.ChatID], msg)
	// A chat holding more than the cap is holding a scrollback someone loaded on
	// purpose. An arriving message adds to it rather than pushing the oldest out.
	if s.msgFloor[msg.ChatID] >= MaxMessagesPerChat {
		s.msgFloor[msg.ChatID]++
	}
	s.markMsgDirtyLocked(msg.ChatID, msg.ID)
	if chat, ok := s.chats[msg.ChatID]; ok {
		m := msg
		chat.LastMessage = &m
		s.chats[msg.ChatID] = chat
		s.orderDirty = true // newer last-message moves the chat in the list
		if sharedPtsBox(chat.Peer) {
			s.msgChat[msg.ID] = msg.ChatID
		}
		s.markDirtyLocked(msg.ChatID) // write-behind: last-message persists on flush
	}
	s.capMessagesLocked(msg.ChatID)
	return true
}

// AdvanceAppliedPosition compares and records under one lock, so the decision
// and the record cannot disagree.
func (s *SQLiteStore) AdvanceAppliedPosition(chatID int64, msgID, position int) bool {
	if position == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages[chatID] {
		if s.messages[chatID][i].ID == msgID {
			if position <= s.messages[chatID][i].AppliedPosition {
				return false
			}
			s.messages[chatID][i].AppliedPosition = position
			s.markMsgDirtyLocked(chatID, msgID)
			return true
		}
	}
	return true
}

// UpdateMessageText replaces a message's text and its entities together. They
// must move as a unit: entity offsets address the text they were parsed from,
// so keeping the old ones would leave them pointing at characters that changed.
//
// The edit marker is not touched here. Whether an edit earns the "edited" label
// is Telegram's to say, and it says so separately (#269).
func (s *SQLiteStore) UpdateMessageText(chatID int64, msgID int, text string, entities []domain.MessageEntity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages[chatID] {
		if s.messages[chatID][i].ID == msgID {
			m := &s.messages[chatID][i]
			// A caption can legitimately be removed, and a broken delivery looks
			// exactly the same from here. Rather than guess, say so and carry on:
			// the counts and the position are enough to tell the two apart in a
			// log, and the text itself never goes into one (#80).
			if text == "" && m.Text != "" {
				s.log.Warn("stored text replaced by an empty one",
					zap.Int64("chat_id", chatID),
					zap.Int("msg_id", msgID),
					zap.Int("was_len", len([]rune(m.Text))),
					zap.Int("now_len", 0),
					zap.Bool("media", m.Media != nil || m.Photo != nil || m.Document != nil),
					zap.Int("position", m.AppliedPosition),
				)
			}
			m.Text = text
			cp := make([]domain.MessageEntity, len(entities))
			copy(cp, entities)
			m.Entities = cp
			s.markMsgDirtyLocked(chatID, msgID)
			return
		}
	}
}

// MarkMessageEdited records the edit time and whether the label is hidden.
func (s *SQLiteStore) MarkMessageEdited(chatID int64, msgID int, editDate time.Time, hidden bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages[chatID] {
		if s.messages[chatID][i].ID == msgID {
			t := editDate
			s.messages[chatID][i].EditDate = &t
			s.messages[chatID][i].EditHidden = hidden
			s.markMsgDirtyLocked(chatID, msgID)
			return
		}
	}
}

func (s *SQLiteStore) UpdateMessageReactions(chatID int64, msgID int, reactions []domain.Reaction, via string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages[chatID] {
		if s.messages[chatID][i].ID == msgID {
			cp := make([]domain.Reaction, len(reactions))
			copy(cp, reactions)
			s.traceReactionChangeLocked(chatID, msgID, via, s.messages[chatID][i].Reactions, cp)
			s.messages[chatID][i].Reactions = cp
			s.markMsgDirtyLocked(chatID, msgID)
			return
		}
	}
}

// UpdateMessageMedia replaces the photo/document refs of a cached message. A nil
// ref leaves that field unchanged.
func (s *SQLiteStore) UpdateMessageMedia(chatID int64, msgID int, photo *domain.PhotoRef, document *domain.DocumentRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages[chatID] {
		if s.messages[chatID][i].ID == msgID {
			if photo != nil {
				s.messages[chatID][i].Photo = photo
			}
			if document != nil {
				s.messages[chatID][i].Document = document
			}
			s.markMsgDirtyLocked(chatID, msgID)
			return
		}
	}
}

// ReplaceMessage overwrites a stored message with msg, fields and all. Unlike
// the field-wise updates it can clear EditDate, which a rolled-back edit must
// do: the message was never edited (#118).
func (s *SQLiteStore) ReplaceMessage(chatID int64, msg domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages[chatID] {
		if s.messages[chatID][i].ID == msg.ID {
			s.traceReactionChangeLocked(chatID, msg.ID, "ReplaceMessage", s.messages[chatID][i].Reactions, msg.Reactions)
			s.messages[chatID][i] = msg
			s.markMsgDirtyLocked(chatID, msg.ID)
			return
		}
	}
}

func (s *SQLiteStore) RemoveMessage(chatID int64, msgID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.messages[chatID]
	for i, m := range msgs {
		if m.ID == msgID {
			s.messages[chatID] = append(msgs[:i], msgs[i+1:]...)
			delete(s.msgChat, msgID)
			s.markMsgDeletedLocked(chatID, msgID)
			return
		}
	}
}

func (s *SQLiteStore) RemoveMessages(chatID int64, msgIDs []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeMessagesLocked(chatID, msgIDs)
}

// removeMessagesLocked drops the given message IDs from one chat, the msgChat
// index and the database. IDs the chat does not hold in memory are queued for
// deletion all the same: a chat that was not opened this session holds nothing
// in memory, and leaving its row behind resurrects the message on the next open.
// Caller holds the lock.
func (s *SQLiteStore) removeMessagesLocked(chatID int64, msgIDs []int) {
	toRemove := make(map[int]struct{}, len(msgIDs))
	for _, id := range msgIDs {
		toRemove[id] = struct{}{}
		// Only drop the index entry if it points here: message IDs are unique
		// within the shared pts box, but a channel numbers its own and may reuse
		// a number that belongs to a private chat.
		if cid, ok := s.msgChat[id]; ok && cid == chatID {
			delete(s.msgChat, id)
		}
		s.markMsgDeletedLocked(chatID, id)
	}
	msgs := s.messages[chatID]
	if len(msgs) == 0 {
		return
	}
	kept := msgs[:0]
	for _, m := range msgs {
		if _, remove := toRemove[m.ID]; remove {
			continue
		}
		kept = append(kept, m)
	}
	s.messages[chatID] = kept
}

// resolveChatsByMsgIDLocked finds the owning chat of message IDs the in-memory
// index does not cover, by looking them up on disk. Only shared-pts-box chats
// count: IDs are globally unique there, whereas a channel's ID space is its own
// and could collide by number. Caller holds the lock.
func (s *SQLiteStore) resolveChatsByMsgIDLocked(msgIDs []int) map[int64][]int {
	if len(msgIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(msgIDs))
	for _, id := range msgIDs {
		args = append(args, id)
	}
	q := `SELECT chat_id, msg_id FROM messages WHERE msg_id IN (?` + strings.Repeat(",?", len(msgIDs)-1) + `)`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		s.log.Error("resolve chats by message id failed", zap.Error(err))
		return nil
	}
	defer func() { _ = rows.Close() }()

	byChat := make(map[int64][]int)
	for rows.Next() {
		var chatID int64
		var msgID int
		if err := rows.Scan(&chatID, &msgID); err != nil {
			s.log.Error("scan message owner failed", zap.Error(err))
			return nil
		}
		if chat, ok := s.chats[chatID]; !ok || !sharedPtsBox(chat.Peer) {
			continue
		}
		byChat[chatID] = append(byChat[chatID], msgID)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("iterate message owners failed", zap.Error(err))
		return nil
	}
	return byChat
}

// RemoveMessagesByID resolves each message ID to its owning chat and removes it
// there, returning the affected chat IDs. Used for the Telegram non-channel
// delete that carries message IDs but no peer context (issue #72). The in-memory
// index only covers chats touched this session, so the rest are resolved on disk.
func (s *SQLiteStore) RemoveMessagesByID(msgIDs []int) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	byChat := make(map[int64][]int)
	var unindexed []int
	for _, id := range msgIDs {
		if cid, ok := s.msgChat[id]; ok {
			byChat[cid] = append(byChat[cid], id)
			continue
		}
		unindexed = append(unindexed, id)
	}
	for cid, ids := range s.resolveChatsByMsgIDLocked(unindexed) {
		byChat[cid] = append(byChat[cid], ids...)
	}
	affected := make([]int64, 0, len(byChat))
	for cid, ids := range byChat {
		s.removeMessagesLocked(cid, ids)
		affected = append(affected, cid)
	}
	return affected
}
