package tg

import (
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// reactionsFlag is the bit of tg.Message.Flags that says the reactions field
// is present (see tg.Message.SetReactions).
const reactionsFlag = 20

// yieldPairedEditReactions drops the reaction set from a hidden edit when the
// same envelope also carries an UpdateMessageReactions for that message: both
// describe one change, and the reactions update is the one that decides (#248).
//
// The pair has to be settled here, on the whole envelope, because nothing
// downstream sees them together. gotd hands the reactions update, which has no
// pts, to the dispatcher at once, while the edit goes through its pts box -
// for a channel on a goroutine of its own - and may be applied after it. The
// stripped edit then reads as one that says nothing about reactions, which
// the edit path leaves alone.
//
// This rests on what was observed rather than on anything Telegram documents.
// In a supergroup, the reply to our own messages.sendReaction carried an edit
// whose set was empty beside a reactions update that had our reaction in it,
// while an edit caused by someone else's reaction carried the right set. If
// Telegram turns out to mean the edit's copy, this is the rule to revisit.
//
// An edit alone keeps its set: in 1:1 chats, and in large channels where a
// min set arrives only as an edit, it is the only statement there is. Pairs
// split across envelopes, or replayed by getDifference, never reach this hook
// together and are not covered.
func yieldPairedEditReactions(log *zap.Logger, u tg.UpdatesClass) {
	var upds []tg.UpdateClass
	switch u := u.(type) {
	case *tg.Updates:
		upds = u.Updates
	case *tg.UpdatesCombined:
		upds = u.Updates
	default:
		return
	}
	type msgKey struct {
		chatID int64
		msgID  int
	}
	stated := make(map[msgKey]bool)
	for _, upd := range upds {
		if r, ok := upd.(*tg.UpdateMessageReactions); ok {
			stated[msgKey{peerIDFromPeer(r.Peer), r.MsgID}] = true
		}
	}
	if len(stated) == 0 {
		return
	}
	for _, upd := range upds {
		var raw tg.MessageClass
		var pts int
		switch e := upd.(type) {
		case *tg.UpdateEditMessage:
			raw, pts = e.Message, e.Pts
		case *tg.UpdateEditChannelMessage:
			raw, pts = e.Message, e.Pts
		default:
			continue
		}
		m, ok := raw.(*tg.Message)
		if !ok {
			continue
		}
		mr, has := m.GetReactions()
		if !has || !stated[msgKey{extractPeerID(m), m.ID}] {
			continue
		}
		log.Debug("reaction: edit yields",
			zap.Int64("chat_id", extractPeerID(m)), zap.Int("msg_id", m.ID), zap.Int("pts", pts),
			zap.String("dropped", formatTGReactions(mr)))
		m.Flags.Unset(reactionsFlag)
		m.Reactions = tg.MessageReactions{}
	}
}
