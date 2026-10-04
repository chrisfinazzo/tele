package core

import (
	"context"

	"github.com/sorokin-vladimir/tele/internal/domain"
)

// SetTyping reports a composing action in a history: a chat, or in a forum the
// topic being written in. It carries no state: the indicator is ephemeral and
// belongs to whoever is watching.
func (o *Owner) SetTyping(ctx context.Context, h domain.HistoryKey, action domain.TypingAction) error {
	peer, err := o.peer(h.ChatID)
	if err != nil {
		return err
	}
	return o.client.SetTyping(ctx, peer, h.TopicID, action)
}

// SaveDraft syncs the composer's unsent text to Telegram, clearing it when text
// is empty. A draft belongs to the history it was typed in, which in a forum is
// a topic. The local draft is stored either way: it is what the user typed, and
// a failed sync must not lose it.
func (o *Owner) SaveDraft(ctx context.Context, h domain.HistoryKey, text string) error {
	peer, err := o.peer(h.ChatID)
	if err != nil {
		return err
	}
	o.state.ApplyDraft(h.ChatID, h.TopicID, text)
	return o.client.SaveDraft(ctx, peer, h.TopicID, text)
}
