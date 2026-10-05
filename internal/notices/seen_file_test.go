package notices_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/notices"
)

// A notice is about the machine rather than the account, so what was seen is
// kept apart from the account's database, which goes with every log out (#297).
func TestFileSeen_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notices.json")

	seen, existed := notices.NewFileSeen(path)
	assert.False(t, existed)
	assert.False(t, seen.IsSeen("some-notice"))
	seen.MarkSeen("some-notice")
	assert.True(t, seen.IsSeen("some-notice"))

	reopened, existed := notices.NewFileSeen(path)
	assert.True(t, existed)
	assert.True(t, reopened.IsSeen("some-notice"))
	assert.False(t, reopened.IsSeen("other-notice"))
}

// A file that cannot be read is a notice shown once more, never a start that
// fails over it.
func TestFileSeen_AnUnreadableFileSeesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notices.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	seen, _ := notices.NewFileSeen(path)

	assert.False(t, seen.IsSeen("some-notice"))
	seen.MarkSeen("some-notice")
	reopened, _ := notices.NewFileSeen(path)
	assert.True(t, reopened.IsSeen("some-notice"), "marking rewrites the file whole")
}

// What an earlier build recorded in the account's database is carried over
// once, so an upgrade does not show every notice again.
func TestCarryOver_CopiesWhatWasSeenBefore(t *testing.T) {
	old := notices.NewMemorySeen()
	old.MarkSeen("a")
	seen, _ := notices.NewFileSeen(filepath.Join(t.TempDir(), "notices.json"))

	notices.CarryOver(seen, old, []string{"a", "b"})

	assert.True(t, seen.IsSeen("a"))
	assert.False(t, seen.IsSeen("b"))
}
