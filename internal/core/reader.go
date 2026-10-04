package core

import (
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

// projectionReader is what the projection registry reads from. The store
// supplies chats, messages and folders; the owner's queue supplies pending
// sends.
//
// The two have different owners on purpose: the queue is not part of the
// account cache, so store.Store does not grow outbox methods and MemoryStore is
// not asked to implement queue semantics (#193).
//
// It holds the owner rather than the queue because the queue is set after the
// registry is built, and rebuilding the registry would drop live subscriptions.
type projectionReader struct {
	store.Store
	owner *Owner
}

// reader is the owner's own projection reader, for the places that build a
// projection outside the registry.
func (o *Owner) reader() projectionReader {
	return projectionReader{Store: o.state.Store(), owner: o}
}

// Outbox returns a history's queued sends, or nothing while no queue is set —
// the case for an owner built without one, as tests do.
func (r projectionReader) Outbox(h domain.HistoryKey) []domain.OutboxEntry {
	if r.owner == nil || r.owner.outbox == nil {
		return nil
	}
	return r.owner.outbox.ForHistory(h)
}
