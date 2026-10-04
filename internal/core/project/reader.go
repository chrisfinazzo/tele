package project

import "github.com/sorokin-vladimir/tele/internal/domain"

// Reader is the read-only view of domain state a builder needs. store.Store
// supplies all of it but the queue; internal/core composes the two.
type Reader interface {
	Chats() []domain.Chat
	GetChat(id int64) (domain.Chat, bool)
	Messages(h domain.HistoryKey) []domain.Message
	FolderFilters() []domain.FolderFilter
	// Outbox returns a chat's queued sends in submission order. It is not on
	// store.Store because the queue is not part of the account cache: the
	// composite reader in internal/core supplies it (#193).
	Outbox(chatID int64) []domain.OutboxEntry
}
