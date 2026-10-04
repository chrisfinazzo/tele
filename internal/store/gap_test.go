package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

func gapStore(t *testing.T) store.Store {
	t.Helper()
	s := store.NewMemory()
	s.SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7, Type: domain.PeerChannel}})
	return s
}

func TestGap_AChatWithNoRecordHasNone(t *testing.T) {
	s := gapStore(t)

	_, ok := s.Gap(7)

	assert.False(t, ok)
}

func TestMarkGap_RecordsWhereTheHoleOpens(t *testing.T) {
	s := gapStore(t)

	s.MarkGap(7, 300)

	after, ok := s.Gap(7)
	require.True(t, ok)
	assert.Equal(t, 300, after)
}

// A busy channel loses a second range while the first is still open. Repairing
// from the earlier position passes through both, so the later one is not worth
// recording and must not overwrite what is there.
func TestMarkGap_KeepsTheEarlierPosition(t *testing.T) {
	s := gapStore(t)

	s.MarkGap(7, 500)
	s.MarkGap(7, 300)
	s.MarkGap(7, 700)

	after, _ := s.Gap(7)
	assert.Equal(t, 300, after)
}

// A gap opens after a message. A chat holding none is one nobody has fetched
// yet, and fetching it is what backfill is for.
func TestMarkGap_RecordsNothingForAChatWithNoMessages(t *testing.T) {
	s := gapStore(t)

	s.MarkGap(7, 0)

	_, ok := s.Gap(7)
	assert.False(t, ok)
}

func TestAdvanceGap_MovesTheRepairForward(t *testing.T) {
	s := gapStore(t)
	s.MarkGap(7, 300)

	assert.True(t, s.AdvanceGap(7, 300, 400))

	after, _ := s.Gap(7)
	assert.Equal(t, 400, after)
}

// A second hole opened underneath a running repair and recorded an earlier
// position. Advancing over it would erase the only evidence it exists, so the
// repair is told its page no longer describes where the work stands.
func TestAdvanceGap_RefusesWhenTheRecordMovedUnderneath(t *testing.T) {
	s := gapStore(t)
	s.MarkGap(7, 300)
	s.MarkGap(7, 100)

	assert.False(t, s.AdvanceGap(7, 300, 400))

	after, _ := s.Gap(7)
	assert.Equal(t, 100, after, "the earlier hole is what is left to close")
}

// The repair finished and closed the gap while a page was still in flight.
func TestAdvanceGap_DoesNotResurrectAClosedGap(t *testing.T) {
	s := gapStore(t)

	assert.False(t, s.AdvanceGap(7, 300, 400))

	_, ok := s.Gap(7)
	assert.False(t, ok)
}

func TestCloseGap_ForgetsAGapThatStandsWhereItWasLeft(t *testing.T) {
	s := gapStore(t)
	s.MarkGap(7, 300)

	assert.True(t, s.CloseGap(7, 300))

	_, ok := s.Gap(7)
	assert.False(t, ok)
}

func TestCloseGap_LeavesAHoleRecordedUnderneathIt(t *testing.T) {
	s := gapStore(t)
	s.MarkGap(7, 300)
	s.MarkGap(7, 100)

	assert.False(t, s.CloseGap(7, 300))

	after, ok := s.Gap(7)
	require.True(t, ok, "a hole nobody closed is still a hole")
	assert.Equal(t, 100, after)
}

func TestClearGap_ForgetsTheChatWhateverItSays(t *testing.T) {
	s := gapStore(t)
	s.MarkGap(7, 300)

	s.ClearGap(7)

	_, ok := s.Gap(7)
	assert.False(t, ok)
}

// The reason the record is on disk: the reporter in #261 ran tele in ten-minute
// sessions. A gap that did not survive the restart would be a gap forever.
func TestMarkGap_SurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := store.NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	s.SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7, Type: domain.PeerChannel}})
	s.MarkGap(7, 300)
	require.NoError(t, s.Close())

	s2, err := store.NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()

	after, ok := s2.Gap(7)
	require.True(t, ok)
	assert.Equal(t, 300, after)
}

func TestTailMessageID_ZeroForAChatWithNothingHeld(t *testing.T) {
	s := gapStore(t)

	assert.Zero(t, s.TailMessageID(7))
}

func TestTailMessageID_IsTheNewestHeld(t *testing.T) {
	s := gapStore(t)
	s.SetMessages(7, []domain.Message{
		{ID: 4, ChatID: 7, Date: time.Unix(4, 0)},
		{ID: 9, ChatID: 7, Date: time.Unix(9, 0)},
	})

	assert.Equal(t, 9, s.TailMessageID(7))
}

// A gap can open on a chat nobody opened this session, whose history is still
// on disk. Reading one number must not drag five hundred messages into memory.
func TestTailMessageID_ReadsDiskWithoutLoadingTheChat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := store.NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	s.SetChat(domain.Chat{ID: 7, Peer: domain.Peer{ID: 7, Type: domain.PeerChannel}})
	s.SetMessages(7, []domain.Message{
		{ID: 4, ChatID: 7, Date: time.Unix(4, 0)},
		{ID: 9, ChatID: 7, Date: time.Unix(9, 0)},
	})
	require.NoError(t, s.Close())

	s2, err := store.NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()

	assert.Equal(t, 9, s2.TailMessageID(7))
	assert.Empty(t, s2.Messages(domain.HistoryKey{ChatID: 7}), "the chat must still be unloaded")
}
