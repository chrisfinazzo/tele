package tg

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"

	"github.com/sorokin-vladimir/tele/internal/domain"
)

// The set the reply to messages.sendReaction states is what the owner judges
// our pick against (#248). Only a reactions update states it: an edit's copy
// yields to one, and a min set cannot say which reaction is ours.
func TestConfirmedReactions(t *testing.T) {
	var rc tg.ReactionCount
	rc.SetChosenOrder(0)
	rc.Reaction = &tg.ReactionEmoji{Emoticon: "👍"}
	rc.Count = 1
	set := tg.MessageReactions{Results: []tg.ReactionCount{rc}}
	ours := []domain.Reaction{{Emoji: "👍", Count: 1, IsChosen: true}}

	t.Run("reactions update for the message", func(t *testing.T) {
		reply := &tg.Updates{Updates: []tg.UpdateClass{
			&tg.UpdateEditChannelMessage{Message: &tg.Message{ID: 7}},
			&tg.UpdateMessageReactions{MsgID: 7, Reactions: set},
		}}
		assert.Equal(t, ours, confirmedReactions(reply, 7))
	})

	t.Run("in a short envelope", func(t *testing.T) {
		reply := &tg.UpdateShort{Update: &tg.UpdateMessageReactions{MsgID: 7, Reactions: set}}
		assert.Equal(t, ours, confirmedReactions(reply, 7))
	})

	t.Run("an empty set is stated", func(t *testing.T) {
		reply := &tg.Updates{Updates: []tg.UpdateClass{
			&tg.UpdateMessageReactions{MsgID: 7, Reactions: tg.MessageReactions{Results: []tg.ReactionCount{}}},
		}}
		got := confirmedReactions(reply, 7)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("only an edit", func(t *testing.T) {
		msg := &tg.Message{ID: 7}
		msg.SetReactions(set)
		reply := &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateEditMessage{Message: msg}}}
		assert.Nil(t, confirmedReactions(reply, 7))
	})

	t.Run("another message", func(t *testing.T) {
		reply := &tg.UpdateShort{Update: &tg.UpdateMessageReactions{MsgID: 8, Reactions: set}}
		assert.Nil(t, confirmedReactions(reply, 7))
	})

	t.Run("a min set", func(t *testing.T) {
		reply := &tg.UpdateShort{Update: &tg.UpdateMessageReactions{MsgID: 7,
			Reactions: tg.MessageReactions{Min: true, Results: []tg.ReactionCount{rc}}}}
		assert.Nil(t, confirmedReactions(reply, 7))
	})
}
