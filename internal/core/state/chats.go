package state

import "github.com/sorokin-vladimir/tele/internal/domain"

// ApplyReadInbox advances a chat's inbound read pointer. Reports false when the
// pointer did not move, so nothing downstream is recomputed for a duplicate or
// stale read receipt.
func (s *State) ApplyReadInbox(chatID int64, maxID int) (Change, bool) {
	if !s.st.UpdateChatReadMaxID(chatID, maxID) {
		return Change{}, false
	}
	c := Change{Kind: ChangeReadInbox, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyReadOutbox advances the peer's read pointer over our own messages. The
// chat is read first because UpdateChatOutboxReadMaxID returns nothing; the
// pre-read reproduces its internal guard so this operation can report whether
// anything moved, matching its siblings.
func (s *State) ApplyReadOutbox(chatID int64, maxID int) (Change, bool) {
	before, ok := s.st.GetChat(chatID)
	if !ok || maxID <= before.ReadOutboxMaxID {
		return Change{}, false
	}
	s.st.UpdateChatOutboxReadMaxID(chatID, maxID)
	c := Change{Kind: ChangeReadOutbox, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyPresence records a contact's online state. Reports false when the state
// did not flip: presence updates stream continuously for every online contact,
// and an unchanged one must cost nothing downstream.
func (s *State) ApplyPresence(userID int64, online bool) (Change, bool) {
	if !s.st.UpdateChatOnline(userID, online) {
		return Change{}, false
	}
	c := Change{Kind: ChangePresence, ChatID: userID, Online: online}
	s.commit(c)
	return c, true
}

// ApplyMute records a mute toggled on another device. Reports false for an
// unknown chat or an unchanged flag.
func (s *State) ApplyMute(chatID int64, muted bool) (Change, bool) {
	chat, ok := s.st.GetChat(chatID)
	if !ok || chat.IsMuted == muted {
		return Change{}, false
	}
	s.st.SetChatMuted(chatID, muted)
	c := Change{Kind: ChangeMute, ChatID: chatID, Muted: muted}
	s.commit(c)
	return c, true
}

// ApplyReactionsRead clears a chat's unread-reaction badge.
func (s *State) ApplyReactionsRead(chatID int64) (Change, bool) {
	if _, ok := s.st.GetChat(chatID); !ok {
		return Change{}, false
	}
	s.st.SetChatReactionsRead(chatID)
	c := Change{Kind: ChangeReactionsRead, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyMentionsRead clears a chat's unread-mention badge.
func (s *State) ApplyMentionsRead(chatID int64) (Change, bool) {
	if _, ok := s.st.GetChat(chatID); !ok {
		return Change{}, false
	}
	s.st.SetChatMentionsRead(chatID)
	c := Change{Kind: ChangeMentionsRead, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyChatRead marks a whole chat read: the unread count and the manual mark
// both clear. Distinct from ApplyReadInbox, which advances the pointer to one
// message and reduces the count by what that pointer passed — a maxID of zero
// means "everything" and has no message to advance to.
func (s *State) ApplyChatRead(chatID int64) (Change, bool) {
	chat, ok := s.st.GetChat(chatID)
	if !ok {
		return Change{}, false
	}
	chat.UnreadCount = 0
	chat.UnreadMark = false
	// The pointer moves too, or the chat opens with a "New messages" divider
	// above messages the user just declared read.
	if chat.LastMessage != nil && chat.LastMessage.ID > chat.ReadInboxMaxID {
		chat.ReadInboxMaxID = chat.LastMessage.ID
	}
	s.st.SetChat(chat)
	c := Change{Kind: ChangeReadInbox, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyArchived moves a chat into or out of the Archive folder. Reports false
// for an unknown chat or an unchanged flag.
func (s *State) ApplyArchived(chatID int64, archived bool) (Change, bool) {
	chat, ok := s.st.GetChat(chatID)
	if !ok || chat.IsArchived == archived {
		return Change{}, false
	}
	s.st.SetChatArchived(chatID, archived)
	c := Change{Kind: ChangeArchived, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyUnreadMark sets or clears the manual unread mark on a chat.
func (s *State) ApplyUnreadMark(chatID int64, mark bool) (Change, bool) {
	chat, ok := s.st.GetChat(chatID)
	if !ok || chat.UnreadMark == mark {
		return Change{}, false
	}
	s.st.SetChatUnreadMark(chatID, mark)
	c := Change{Kind: ChangeUnreadMark, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyFolderMembership adds or removes a chat from a folder filter's include
// list. Reports false when the filter is unknown or already in that state.
func (s *State) ApplyFolderMembership(filterID int, chatID int64, add bool) (Change, bool) {
	filters := s.st.FolderFilters()
	idx := -1
	for i := range filters {
		if filters[i].ID == filterID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Change{}, false
	}
	next := toggleChatID(filters[idx].IncludePeers, chatID, add)
	if len(next) == len(filters[idx].IncludePeers) {
		return Change{}, false
	}
	filters[idx].IncludePeers = next
	s.st.SetFolderFilters(filters)
	c := Change{Kind: ChangeFolders, ChatID: chatID}
	s.commit(c)
	return c, true
}

// toggleChatID returns ids with id added or removed.
func toggleChatID(ids []int64, id int64, add bool) []int64 {
	out := make([]int64, 0, len(ids)+1)
	found := false
	for _, e := range ids {
		if e == id {
			found = true
			if !add {
				continue
			}
		}
		out = append(out, e)
	}
	if add && !found {
		out = append(out, id)
	}
	return out
}

// ApplyDraft records a draft changed on another device, or cleared server-side
// on send (#62).
func (s *State) ApplyDraft(chatID int64, topicID int, text string) (Change, bool) {
	h := s.historyFor(chatID, topicID)
	if h.TopicID != 0 {
		// In a forum a draft is the topic's: the forum has no composer.
		s.st.SetTopicDraft(h.ChatID, h.TopicID, text)
	} else {
		s.st.SetChatDraft(chatID, text)
	}
	c := Change{Kind: ChangeDraft, ChatID: chatID, TopicID: h.TopicID, Draft: text}
	s.commit(c)
	return c, true
}

// ApplyTyping publishes a typing indicator. It has no persisted state: the
// typing label is ephemeral and belongs to the chat view, not to the account.
func (s *State) ApplyTyping(chatID int64, topicID int, action domain.TypingAction) (Change, bool) {
	h := s.historyFor(chatID, topicID)
	c := Change{Kind: ChangeTyping, ChatID: chatID, TopicID: h.TopicID, Typing: action}
	s.commit(c)
	return c, true
}

// historyFor names the history Telegram means by a topic id of a chat. A chat
// the store does not know is treated as no forum.
func (s *State) historyFor(chatID int64, topicID int) domain.HistoryKey {
	chat, ok := s.st.GetChat(chatID)
	if !ok {
		return domain.HistoryKey{ChatID: chatID}
	}
	return chat.History(topicID)
}
