package ui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

func stubWithClip(t *testing.T) *ownerStub {
	t.Helper()
	o := newOwnerStub(store.NewMemory())
	src := filepath.Join(t.TempDir(), "clip.mp4")
	require.NoError(t, os.WriteFile(src, []byte("video"), 0600))
	o.mediaPaths[mediaKey{1, 5, domain.DocFull}] = src
	return o
}

// The downloads that light the status-bar indicator ask to watch their
// progress, under the indicator's own serial, so a frame finds its way back to
// the indicator it belongs to (#204).
func TestDownloadsBehindTheIndicatorAskForProgress(t *testing.T) {
	o := stubWithClip(t)
	restore := SetOpenPathForTest(func(string) {})
	defer restore()

	saveFileCmd(context.Background(), o, 1, 5, domain.DocFull, t.TempDir(), 7)()
	openDocumentCmd(context.Background(), o, 1, 5, t.TempDir(), 3)()

	assert.Equal(t, []string{"7", "3"}, o.saveRefs)
}

// A download nobody sees in the status bar asks for nothing: the prefetch of a
// full photo runs for every photo in the window.
func TestAFullPhotoPrefetchAsksForNoProgress(t *testing.T) {
	o := newOwnerStub(store.NewMemory())

	saveFullPhotoCmd(context.Background(), o, 1, 5, 9, t.TempDir(), true)()

	assert.Equal(t, []string{""}, o.saveRefs)
}

func TestDownloadProgressMovesTheIndicatorItBelongsTo(t *testing.T) {
	m := NewRootModel(false)
	m.statusBar.SetWidth(120)
	serial := m.statusBar.StartDownload("downloading video…")

	nm, _ := m.Update(core.DownloadProgress{Ref: strconv.Itoa(serial), Done: 168 << 20, Total: 400 << 20})

	view := nm.(RootModel).statusBar.View()
	assert.Contains(t, view, "42% · 168/400 MB")
}

func TestDownloadProgressForAnotherDownloadIsIgnored(t *testing.T) {
	m := NewRootModel(false)
	m.statusBar.SetWidth(120)
	stale := m.statusBar.StartDownload("first")
	m.statusBar.StartDownload("second")

	nm, _ := m.Update(core.DownloadProgress{Ref: strconv.Itoa(stale), Done: 1, Total: 2})

	assert.False(t, strings.Contains(nm.(RootModel).statusBar.View(), "%"))
}
