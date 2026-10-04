package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newFileStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLite(filepath.Join(t.TempDir(), "state.db"), zap.NewNop())
	require.NoError(t, err)
	return s
}

func TestMarkMsgDirty_SkipsOptimisticNegativeID(t *testing.T) {
	s := newFileStore(t)
	defer func() { _ = s.Close() }()
	s.SetChat(domain.Chat{ID: 1, Peer: domain.Peer{ID: 1, Type: domain.PeerUser}})

	// A negative id is not a message Telegram ever assigned, so it must never
	// reach the database. Nothing produces one since #195 retired the optimistic
	// sentinels, but the guard is what keeps that true.
	s.AppendMessage(domain.Message{ID: -100, ChatID: 1, Text: "sending", Date: time.Unix(1, 0)})

	s.mu.Lock()
	dirty := len(s.dirtyMsgs[1])
	s.mu.Unlock()
	assert.Equal(t, 0, dirty, "negative-id messages must never be queued for persistence")
}

// The flusher drains the delete queue under the lock and writes without it.
// Opening a chat inside that window must not read the deleted row back.
func TestLoadMessages_IgnoresDeletesInFlight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	s, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	s.SetChat(domain.Chat{ID: 3, Peer: domain.Peer{ID: 3, Type: domain.PeerUser}})
	s.SetMessages(3, []domain.Message{
		{ID: 1, ChatID: 3, Text: "kept", Date: time.Unix(1, 0)},
		{ID: 2, ChatID: 3, Text: "deleted", Date: time.Unix(2, 0)},
	})
	require.NoError(t, s.Close())

	s2, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	s2.RemoveMessagesByID([]int{2})

	// Take the snapshot the flusher takes, then open the chat before the
	// transaction lands.
	s2.mu.Lock()
	writes := s2.snapshotMessageWritesLocked()
	s2.mu.Unlock()

	s2.LoadMessages(domain.HistoryKey{ChatID: 3})
	got := s2.Messages(domain.HistoryKey{ChatID: 3})
	require.Len(t, got, 1, "a delete in flight must still hide the row on disk")
	assert.Equal(t, 1, got[0].ID)

	s2.flushMessageRows(writes)
}

func TestLoadMessages_DoesNotQueueWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	s, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	s.SetChat(domain.Chat{ID: 2, Peer: domain.Peer{ID: 2, Type: domain.PeerUser}})
	s.SetMessages(2, []domain.Message{{ID: 1, ChatID: 2, Text: "hi", Date: time.Unix(1, 0)}})
	require.NoError(t, s.Close())

	s2, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	s2.LoadMessages(domain.HistoryKey{ChatID: 2})

	s2.mu.Lock()
	dirty := len(s2.dirtyMsgs[2])
	del := len(s2.deletedMsgs[2])
	s2.mu.Unlock()
	assert.Equal(t, 0, dirty, "loading from disk must not queue upserts")
	assert.Equal(t, 0, del, "loading a within-cap tail must not queue deletes")
}
