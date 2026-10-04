package state

import "github.com/sorokin-vladimir/tele/internal/store"

// Apply routes a raw Telegram event to the domain operation that describes it,
// returning the resulting change and whether anything changed at all.
//
// It lives beside the operations rather than in the owner because deciding what
// an event means is domain knowledge: the owner's job is to hold the connection
// and publish, not to interpret.
func Apply(s *State, evt store.Event) (Change, bool) {
	switch evt.Kind {
	case store.EventNewMessage:
		return s.ApplyIncoming(evt.Message)
	case store.EventReadInbox:
		return s.ApplyReadInbox(evt.ChatID, evt.ReadMaxID)
	case store.EventReadOutbox:
		return s.ApplyReadOutbox(evt.ChatID, evt.ReadMaxID)
	case store.EventEditMessage:
		return s.ApplyEdit(evt.Message)
	case store.EventReactionsUpdate:
		return s.ApplyReactions(evt.ChatID, evt.MsgID, evt.Reactions, evt.ReactionsUnread, "reactions update")
	case store.EventDeleteMessages:
		return s.ApplyDelete(evt.ChatID, evt.MsgIDs)
	case store.EventUserPresence:
		return s.ApplyPresence(evt.ChatID, evt.Online)
	case store.EventMuteUpdate:
		return s.ApplyMute(evt.ChatID, evt.Muted)
	case store.EventDraftMessage:
		return s.ApplyDraft(evt.ChatID, evt.TopicID, evt.Draft)
	case store.EventTyping:
		return s.ApplyTyping(evt.ChatID, evt.TopicID, evt.TypingAction)
	}
	return Change{}, false
}
