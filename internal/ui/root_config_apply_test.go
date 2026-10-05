package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/config"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/telerr"
	"github.com/sorokin-vladimir/tele/internal/ui/components"
	"github.com/sorokin-vladimir/tele/internal/ui/media"
	"github.com/sorokin-vladimir/tele/internal/ui/theme"
)

// applyModel builds a model over a real config file, wired to reload from it the
// way the app wires it. Everything here goes through the file, because that is
// the only way a change reaches a running tele and the point is to test that.
func applyModel(t *testing.T, body string) (RootModel, *config.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("telegram:\n  api_id: 1\n  api_hash: x\n"+body), 0600))
	store, err := config.NewStore(path, t.TempDir())
	require.NoError(t, err)

	t.Cleanup(func() { theme.SetSlots(theme.Slots{Dark: theme.TeleDark, Light: theme.TeleLight}) })

	m := NewRootModel(50, false).
		WithConfig(store.Current()).
		WithConfigReload(func() (*config.Config, error) {
			if err := store.Reload(); err != nil {
				return nil, err
			}
			return store.Current(), nil
		})
	m.width, m.height = 100, 40
	m.toasts.SetSize(100, 40)
	return m, store
}

// takesHoldOnReload writes value to the setting at key and reloads, and checks
// that what the model is running on - never the config, which would only prove
// the file was re-read - now reads want. A setting declared to take effect
// without a restart, and then quietly not taking effect, is worse than one that
// says it needs a restart: the person changes it, sees nothing, and concludes
// the setting is broken (#239).
func takesHoldOnReload(t *testing.T, key string, value any, observe func(RootModel) any, want any) {
	t.Helper()
	m, store := applyModel(t, "")
	require.NotEqual(t, want, observe(m), "%s already looks applied; the test proves nothing", key)

	require.NoError(t, store.Set(key, value))
	m, _ = m.reloadFromDisk()

	assert.Equal(t, want, observe(m))
}

func TestTakesHold_ui_toasts_error_zone(t *testing.T) {
	takesHoldOnReload(t, "ui.toasts.error_zone", "top-right",
		func(m RootModel) any { return m.toasts.ZoneOf(components.ToastError) }, components.ZoneTopRight)
}

func TestTakesHold_ui_toasts_notify_zone(t *testing.T) {
	takesHoldOnReload(t, "ui.toasts.notify_zone", "bottom-right",
		func(m RootModel) any { return m.toasts.ZoneOf(components.ToastNotify) }, components.ZoneBottomRight)
}

func TestTakesHold_ui_toasts_max_visible(t *testing.T) {
	takesHoldOnReload(t, "ui.toasts.max_visible", 7,
		func(m RootModel) any { return m.toasts.MaxVisible() }, 7)
}

// The interface's half: how much the next chat window asks the owner for. The
// owner's half, how much the next fetch asks Telegram for, is its own test.
func TestTakesHold_ui_history_limit(t *testing.T) {
	takesHoldOnReload(t, "ui.history_limit", 111,
		func(m RootModel) any { return m.historyLimit }, 111)
}

func TestTakesHold_ui_theme(t *testing.T) {
	takesHoldOnReload(t, "ui.theme", "nord",
		func(RootModel) any { return theme.T().Name }, "nord")
}

func TestTakesHold_photos_kitty_placement_cap(t *testing.T) {
	takesHoldOnReload(t, "photos.kitty_placement_cap", 33,
		func(m RootModel) any { return m.kittyCap }, 33)
}

func TestTakesHold_photos_max_long_side_px(t *testing.T) {
	takesHoldOnReload(t, "photos.max_long_side_px", 1234,
		func(m RootModel) any { return m.chat.MaxMediaPx() }, 1234)
}

// mediaOwner refuses every download and remembers which were asked for.
type mediaOwner struct {
	Owner
	slots []domain.MediaSlot
}

func (o *mediaOwner) FetchMedia(_ context.Context, _ int64, _ int, slot domain.MediaSlot) (string, error) {
	o.slots = append(o.slots, slot)
	return "", &telerr.Error{Kind: telerr.NotFound}
}

func (o *mediaOwner) SaveMedia(_ context.Context, _ int64, _ int, slot domain.MediaSlot, _, _ string) (string, error) {
	o.slots = append(o.slots, slot)
	return "", &telerr.Error{Kind: telerr.NotFound}
}

// runAll runs cmd and every command it batches.
func runAll(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			runAll(c)
		}
	}
}

func TestTakesHold_photos_eager_full_quality(t *testing.T) {
	m, store := applyModel(t, "photos:\n  eager_full_quality: true\n")
	owner := &mediaOwner{}
	m.owner = owner
	photo := []domain.Message{{ID: 10, ChatID: 1,
		Photo: &domain.PhotoRef{ID: 321, FullThumbSize: "y"}}}
	runAll(m.pendingDownloadCmds(photo))
	require.Contains(t, owner.slots, domain.PhotoFull, "the full photo was not fetched while the setting was on")

	require.NoError(t, store.Set("photos.eager_full_quality", false))
	m, _ = m.reloadFromDisk()
	owner.slots = nil
	runAll(m.pendingDownloadCmds(photo))

	assert.NotContains(t, owner.slots, domain.PhotoFull, "the next chat opened still fetched the full photo")
}

// A person editing the file in another window gets the same result as a person
// using the overlay, because both arrive the same way.
func TestReload_PicksUpAnEditMadeOutsideTheApp(t *testing.T) {
	m, store := applyModel(t, "ui:\n  history_limit: 50\n")

	require.NoError(t, os.WriteFile(store.Path(), []byte(
		"telegram:\n  api_id: 1\n  api_hash: x\nui:\n  history_limit: 175\n"), 0600))
	m, _ = m.reloadFromDisk()

	assert.Equal(t, 175, m.historyLimit)
	assert.Equal(t, 175, m.cfg.UI.HistoryLimit)
}

// A startup setting is written and kept, and the running app goes on with what
// it started with. Swapping the renderer under images already transmitted to the
// terminal is not a setting taking effect, it is a mess.
func TestReload_StartupSettingDoesNotChangeTheRunningApp(t *testing.T) {
	m, store := applyModel(t, "photos:\n  mode: blocks\n")
	m = m.WithImageMode(media.ModeBlocks)

	require.NoError(t, store.Set("photos.mode", "kitty"))
	m, _ = m.reloadFromDisk()

	assert.Equal(t, media.ModeBlocks, m.imageMode, "still what it started with")
	assert.Equal(t, "kitty", m.cfg.Photos.Mode, "and the change is kept for next time")
}

// Toasts already on screen move with the setting. Leaving them where they were
// would draw the stack in two corners at once until they expired, which reads as
// a bug rather than as a setting taking effect.
func TestReload_MovingTheZoneTakesTheToastsAlreadyShowing(t *testing.T) {
	m, store := applyModel(t, "")
	m.toasts.Add(components.ToastError, "something went wrong")
	require.Equal(t, components.ZoneBottomRight, m.toasts.ZoneOf(components.ToastError))

	require.NoError(t, store.Set("ui.toasts.error_zone", "top-right"))
	m, _ = m.reloadFromDisk()

	m.SettleToastsForTest()
	zones := m.toasts.Zones()
	require.NotEmpty(t, zones, "the toast is still on screen")
	// The top-right zone is stamped one row down from the top edge; the
	// bottom-right one would be near row 40 in a 40-row viewport.
	assert.Equal(t, 1, zones[0].Top, "and it has moved up with the setting")
}

// A reload that fails leaves the app running on what it had. A config that
// stopped parsing is a reason to say so, not a reason to lose the settings that
// were working.
func TestReload_AFailureKeepsTheRunningConfigAndSaysSo(t *testing.T) {
	m, store := applyModel(t, "ui:\n  history_limit: 120\n")
	require.NoError(t, os.WriteFile(store.Path(), []byte("ui:\n  history_limit: 50\n   theme: nord\n"), 0600))

	m, cmd := m.reloadFromDisk()

	assert.Equal(t, 120, m.historyLimit, "still running on what it had")
	require.NotNil(t, cmd)
	m.SettleToastsForTest()
	zones := m.toasts.Zones()
	require.NotEmpty(t, zones)
	assert.Contains(t, toastText(zones[0].Block), "not reloaded")
}

// The overlay writes and the reload applies, so a value set through the store
// reaches the running model without anybody editing anything by hand.
func TestReload_AppliesAValueWrittenThroughTheStore(t *testing.T) {
	m, store := applyModel(t, "")

	require.NoError(t, store.Set("ui.toasts.max_visible", 9))
	m, _ = m.reloadFromDisk()

	assert.Equal(t, 9, m.toasts.MaxVisible())
}

// Resetting a setting is writing absence, and absence applies like any other
// value: the model goes back to the default.
func TestReload_ResettingASettingAppliesTheDefault(t *testing.T) {
	m, store := applyModel(t, "ui:\n  history_limit: 175\n")
	require.Equal(t, 175, m.historyLimit)

	require.NoError(t, store.Set("ui.history_limit", nil))
	m, _ = m.reloadFromDisk()

	assert.Equal(t, 50, m.historyLimit)
}
