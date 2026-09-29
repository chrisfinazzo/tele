package store_test

import (
	"testing"
	"time"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemory_AppendMessage_ReportsWhetherItWasNew(t *testing.T) {
	s := store.NewMemory()
	msg := domain.Message{ID: 1, ChatID: 5, Text: "hi"}

	assert.True(t, s.AppendMessage(msg), "the first copy is new")
	assert.False(t, s.AppendMessage(msg), "the second is the same message again")
	assert.Len(t, s.Messages(5), 1)
}

// A pending bubble has no id yet, so two of them are two different sends rather
// than one message arriving twice.
func TestMemory_AppendMessage_UnnumberedIsAlwaysNew(t *testing.T) {
	s := store.NewMemory()
	assert.True(t, s.AppendMessage(domain.Message{ChatID: 5, Text: "one"}))
	assert.True(t, s.AppendMessage(domain.Message{ChatID: 5, Text: "two"}))
	assert.Len(t, s.Messages(5), 2)
}

func TestMemory_AdvanceAppliedPosition(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "hi"})

	assert.True(t, s.AdvanceAppliedPosition(5, 1, 20), "nothing has been applied yet")
	assert.False(t, s.AdvanceAppliedPosition(5, 1, 20), "that one has been applied")
	assert.False(t, s.AdvanceAppliedPosition(5, 1, 19), "and so has a later one")
	assert.True(t, s.AdvanceAppliedPosition(5, 1, 21))
	assert.Equal(t, 21, s.Messages(5)[0].AppliedPosition)
}

// A change from a source with no position cannot be ordered against anything,
// so it passes and records nothing.
func TestMemory_AdvanceAppliedPosition_ZeroPasses(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "hi"})
	require.True(t, s.AdvanceAppliedPosition(5, 1, 20))

	assert.True(t, s.AdvanceAppliedPosition(5, 1, 0))
	assert.Equal(t, 20, s.Messages(5)[0].AppliedPosition, "and leaves the position where it was")
}

func TestMemory_AdvanceAppliedPosition_UnknownMessagePasses(t *testing.T) {
	s := store.NewMemory()
	assert.True(t, s.AdvanceAppliedPosition(5, 404, 20),
		"a message we do not hold has applied nothing, so nothing is out of order")
}

func TestMemory_UpdateMessageMedia(t *testing.T) {
	s := store.NewMemory()
	s.SetMessages(7, []domain.Message{
		{ID: 100, ChatID: 7, Photo: &domain.PhotoRef{ID: 1, FileReference: []byte("old")}},
	})
	fresh := &domain.PhotoRef{ID: 1, FileReference: []byte("new")}
	s.UpdateMessageMedia(7, 100, fresh, nil)
	got := s.Messages(7)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Photo)
	assert.Equal(t, []byte("new"), got[0].Photo.FileReference)
}

func TestMemory_SetGetChat(t *testing.T) {
	s := store.NewMemory()
	chat := domain.Chat{ID: 42, Title: "Test"}
	s.SetChat(chat)
	got, ok := s.GetChat(42)
	assert.True(t, ok)
	assert.Equal(t, chat, got)
}

func TestMemory_GetChat_Missing(t *testing.T) {
	s := store.NewMemory()
	_, ok := s.GetChat(999)
	assert.False(t, ok)
}

func TestMemory_Chats_ReturnsAll(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "A"})
	s.SetChat(domain.Chat{ID: 2, Title: "B"})
	assert.Len(t, s.Chats(), 2)
}

func TestMemory_SetGetMessages(t *testing.T) {
	s := store.NewMemory()
	now := time.Now()
	msgs := []domain.Message{
		{ID: 1, ChatID: 10, Text: "hello", Date: now},
		{ID: 2, ChatID: 10, Text: "world", Date: now},
	}
	s.SetMessages(10, msgs)
	assert.Len(t, s.Messages(10), 2)
	assert.Empty(t, s.Messages(999))
}

func TestMemory_AppendMessage(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "a"})
	s.AppendMessage(domain.Message{ID: 2, ChatID: 5, Text: "b"})
	assert.Len(t, s.Messages(5), 2)
}

func TestMemory_AppendMessage_UpdatesLastMessage(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 5, Title: "domain.Chat"})
	msg := domain.Message{ID: 10, ChatID: 5, Text: "hello"}
	s.AppendMessage(msg)
	chat, ok := s.GetChat(5)
	assert.True(t, ok)
	assert.NotNil(t, chat.LastMessage)
	assert.Equal(t, 10, chat.LastMessage.ID)
	assert.Equal(t, "hello", chat.LastMessage.Text)
	// Second append must replace LastMessage
	msg2 := domain.Message{ID: 11, ChatID: 5, Text: "world"}
	s.AppendMessage(msg2)
	chat, ok = s.GetChat(5)
	assert.True(t, ok)
	assert.Equal(t, 11, chat.LastMessage.ID)
	assert.Equal(t, "world", chat.LastMessage.Text)
}

func TestMemory_AppendMessage_SkipsLastMessageWhenChatMissing(t *testing.T) {
	s := store.NewMemory()
	// chat 99 is not in store — must not panic
	assert.NotPanics(t, func() {
		s.AppendMessage(domain.Message{ID: 1, ChatID: 99, Text: "orphan"})
	})
}

func TestMemory_UpdateMessageText(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "original"})
	s.UpdateMessageText(5, 1, "edited", nil)
	msgs := s.Messages(5)
	require.Len(t, msgs, 1)
	assert.Equal(t, "edited", msgs[0].Text)
	assert.Nil(t, msgs[0].EditDate, "writing text decides nothing about the label")
}

func TestMemory_MarkMessageEdited(t *testing.T) {
	s := store.NewMemory()
	now := time.Now()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "original"})

	s.MarkMessageEdited(5, 1, now, false)
	msgs := s.Messages(5)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0].EditDate)
	assert.True(t, msgs[0].ShowsEdited())

	// The same message, edited again with the label hidden: the time stays
	// recorded and the mark goes away.
	s.MarkMessageEdited(5, 1, now.Add(time.Minute), true)
	msgs = s.Messages(5)
	require.NotNil(t, msgs[0].EditDate)
	assert.False(t, msgs[0].ShowsEdited())
}

func TestMemory_UpdateMessageText_ReplacesEntities(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{
		ID: 1, ChatID: 5, Text: "смотри https://example.com",
		Entities: []domain.MessageEntity{{Type: "url", Offset: 7, Length: 19}},
	})
	s.UpdateMessageText(5, 1, "привет", nil)
	msgs := s.Messages(5)
	require.Len(t, msgs, 1)
	assert.Equal(t, "привет", msgs[0].Text)
	// Stale entities must not survive a text change: their offsets addressed the
	// old text.
	assert.Empty(t, msgs[0].Entities)
}

func TestMemory_UpdateMessageText_SetsNewEntities(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "old"})
	ents := []domain.MessageEntity{{Type: "bold", Offset: 0, Length: 5}}
	s.UpdateMessageText(5, 1, "новый", ents)
	msgs := s.Messages(5)
	require.Len(t, msgs, 1)
	assert.Equal(t, ents, msgs[0].Entities)
}

func TestMemory_UpdateMessageText_NoopWhenMissing(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "msg"})
	assert.NotPanics(t, func() {
		s.UpdateMessageText(5, 999, "x", nil)
	})
	msgs := s.Messages(5)
	assert.Equal(t, "msg", msgs[0].Text)
}

func TestMemory_RemoveMessage(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: -1, ChatID: 5, Text: "sentinel"})
	s.AppendMessage(domain.Message{ID: 2, ChatID: 5, Text: "other"})
	s.RemoveMessage(5, -1)
	msgs := s.Messages(5)
	require.Len(t, msgs, 1)
	assert.Equal(t, 2, msgs[0].ID)
}

func TestMemory_RemoveMessage_NoopWhenMissing(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "msg"})
	assert.NotPanics(t, func() {
		s.RemoveMessage(5, 999)
	})
	assert.Len(t, s.Messages(5), 1)
}

func TestMemory_RemoveMessages_RemovesMatchingIDs(t *testing.T) {
	s := store.NewMemory()
	now := time.Now()
	s.SetMessages(10, []domain.Message{
		{ID: 1, ChatID: 10, Text: "a", Date: now},
		{ID: 2, ChatID: 10, Text: "b", Date: now},
		{ID: 3, ChatID: 10, Text: "c", Date: now},
	})
	s.RemoveMessages(10, []int{1, 3})
	msgs := s.Messages(10)
	require.Len(t, msgs, 1)
	assert.Equal(t, 2, msgs[0].ID)
}

func TestMemory_RemoveMessages_NoopWhenEmpty(t *testing.T) {
	s := store.NewMemory()
	s.RemoveMessages(99, []int{1, 2, 3})
	assert.Empty(t, s.Messages(99))
}

func TestMemory_UpdateMessageReactions_SetsReactions(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "hi"})
	reactions := []domain.Reaction{
		{Emoji: "❤️", Count: 3, IsChosen: true},
		{Emoji: "👍", Count: 1, IsChosen: false},
	}
	s.UpdateMessageReactions(5, 1, reactions, "reactions update")
	msgs := s.Messages(5)
	require.Len(t, msgs, 1)
	assert.Equal(t, reactions, msgs[0].Reactions)
}

func TestMemory_UpdateMessageReactions_NoopWhenMissing(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "hi"})
	assert.NotPanics(t, func() {
		s.UpdateMessageReactions(5, 999, []domain.Reaction{{Emoji: "👍", Count: 1}}, "reactions update")
	})
	msgs := s.Messages(5)
	assert.Empty(t, msgs[0].Reactions)
}

func TestMemory_UpdateChatOnline_SetsOnline(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "Alice"})
	s.UpdateChatOnline(1, true)
	chat, ok := s.GetChat(1)
	assert.True(t, ok)
	assert.True(t, chat.Online)
}

func TestMemory_UpdateChatOnline_SetsOffline(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "Alice", Online: true})
	s.UpdateChatOnline(1, false)
	chat, ok := s.GetChat(1)
	assert.True(t, ok)
	assert.False(t, chat.Online)
}

func TestMemory_UpdateChatOnline_NoopWhenMissing(t *testing.T) {
	s := store.NewMemory()
	assert.NotPanics(t, func() {
		s.UpdateChatOnline(999, true)
	})
}

func TestMemory_UpdateChatOnline_ReturnsTrueOnFlip(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "Alice"})
	assert.True(t, s.UpdateChatOnline(1, true))
}

func TestMemory_UpdateChatOnline_ReturnsFalseWhenUnchanged(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "Alice", Online: true})
	assert.False(t, s.UpdateChatOnline(1, true))
}

func TestMemory_UpdateChatOnline_ReturnsFalseWhenMissing(t *testing.T) {
	s := store.NewMemory()
	assert.False(t, s.UpdateChatOnline(999, true))
}

func TestMemory_UpdateChatReadMaxID_ReturnsTrueWhenAdvanced(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "Alice", ReadInboxMaxID: 5})
	assert.True(t, s.UpdateChatReadMaxID(1, 10))
}

func TestMemory_UpdateChatReadMaxID_ReturnsFalseWhenNotAdvanced(t *testing.T) {
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 1, Title: "Alice", ReadInboxMaxID: 10})
	assert.False(t, s.UpdateChatReadMaxID(1, 10))
}

func TestMemory_UpdateChatReadMaxID_ReturnsFalseWhenMissing(t *testing.T) {
	s := store.NewMemory()
	assert.False(t, s.UpdateChatReadMaxID(999, 10))
}

func TestMemory_UpdateMessageReactions_ReplacesExisting(t *testing.T) {
	s := store.NewMemory()
	s.AppendMessage(domain.Message{ID: 1, ChatID: 5, Text: "hi",
		Reactions: []domain.Reaction{{Emoji: "👍", Count: 2}},
	})
	s.UpdateMessageReactions(5, 1, []domain.Reaction{{Emoji: "❤️", Count: 1}}, "reactions update")
	msgs := s.Messages(5)
	require.Len(t, msgs[0].Reactions, 1)
	assert.Equal(t, "❤️", msgs[0].Reactions[0].Emoji)
}

func TestMemory_FolderFilters_EmptyInitially(t *testing.T) {
	s := store.NewMemory()
	assert.Nil(t, s.FolderFilters())
}

func TestMemory_FolderFilters_SetAndGet(t *testing.T) {
	s := store.NewMemory()
	filters := []domain.FolderFilter{
		{ID: 1, Title: "Work", Groups: true},
		{ID: 2, Title: "Personal", Contacts: true},
	}
	s.SetFolderFilters(filters)
	got := s.FolderFilters()
	require.Len(t, got, 2)
	assert.Equal(t, "Work", got[0].Title)
	assert.Equal(t, "Personal", got[1].Title)
}

func TestMemory_FolderFilters_Replace(t *testing.T) {
	s := store.NewMemory()
	s.SetFolderFilters([]domain.FolderFilter{{ID: 1, Title: "Old"}})
	s.SetFolderFilters([]domain.FolderFilter{{ID: 2, Title: "New"}})
	got := s.FolderFilters()
	require.Len(t, got, 1)
	assert.Equal(t, "New", got[0].Title)
}

// An album's grouped_id used to be stamped on afterwards, by the client that
// sent it. Since #195 the parts arrive from the owner's post-send refresh with
// it already set, so there is nothing to stamp.
