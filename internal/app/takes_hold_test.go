package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/appkey"
	"github.com/sorokin-vladimir/tele/internal/store"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
	"github.com/sorokin-vladimir/tele/internal/ui/media"
)

// A startup setting is read once per process. Each test here changes one in the
// file and reloads, lets the account end, and looks at the account that starts
// next: it has to be opened from what the process started with, not from what
// the file says by then (#239).

// startupKey is the app key every host here resolved, unless a test is about
// the key.
var startupKey = appkey.Key{ID: 7, Hash: "seven"}

// afterReload is a host whose first account ended after the config was
// reloaded, and the account that started in its place.
type afterReload struct {
	rig   testHostRig
	first *account
	next  *account
	// firstDatabase is where the first account's database was open.
	firstDatabase string
	// endpoint is what the next account's connection was made with.
	endpoint internaltg.Endpoint
}

// startNextAfterReload starts a host on a file that says before, rewrites the
// file to say after and reloads it, then ends the first account with a log
// out. It checks that the reload did change the setting at key, so a test that
// passes is not one whose edit the file never took.
func startNextAfterReload(t *testing.T, key appkey.Key, setting, before, after string) afterReload {
	t.Helper()
	h := newTestHost(t, &loggedOutConnection{updates: make(chan store.Event)}, key, before)
	a := h.app
	<-h.endpoints
	was, _ := a.cfgStore.Value(setting)
	require.NoError(t, os.WriteFile(h.path, []byte(after), 0o600))
	_, err := a.reloadConfig()
	require.NoError(t, err)
	now, _ := a.cfgStore.Value(setting)
	require.NotEqual(t, was, now, "the reload did not change %s", setting)

	first, epoch := a.current()
	firstDatabase := databaseFile(t, first)
	ctx, cancel := context.WithCancel(context.Background())
	a.launch(ctx, first, epoch)
	waitForEnd(t, h.delivered)
	var endpoint internaltg.Endpoint
	select {
	case endpoint = <-h.endpoints:
	case <-time.After(3 * time.Second):
		require.Fail(t, "the next account never connected")
	}
	next, _ := a.current()
	t.Cleanup(func() {
		cancel()
		a.switching.Lock()
		next.stop()
		a.switching.Unlock()
	})
	return afterReload{rig: h, first: first, next: next, firstDatabase: firstDatabase, endpoint: endpoint}
}

// databaseFile is where an account's database is open.
func databaseFile(t *testing.T, acct *account) string {
	t.Helper()
	var seq int
	var name, file string
	require.NoError(t, acct.store.DB().QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file))
	resolved, err := filepath.EvalSymlinks(file)
	require.NoError(t, err)
	return resolved
}

// The owner's settings take hold because a reload hands it the new config; the
// owner's own tests start from there. This is the handing over, and it reaches
// the account running now, not the one the process started with.
func TestHost_AReloadReachesTheOwnerOfTheAccountRunningNow(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "ui.history_limit",
		"ui:\n  history_limit: 30\n", "ui:\n  history_limit: 40\n")
	require.NoError(t, os.WriteFile(r.rig.path, []byte("ui:\n  history_limit: 70\n"), 0o600))

	_, err := r.rig.app.reloadConfig()
	require.NoError(t, err)

	assert.Equal(t, 70, r.next.owner.Config().UI.HistoryLimit)
}

func TestTakesHold_telegram_api_id(t *testing.T) {
	key := appkey.Key{ID: 1, Hash: "first"}
	r := startNextAfterReload(t, key, "telegram.api_id",
		"telegram:\n  api_id: 1\n  api_hash: first\n",
		"telegram:\n  api_id: 2\n  api_hash: first\n")

	assert.Equal(t, key, r.endpoint.Key)
}

func TestTakesHold_telegram_api_hash(t *testing.T) {
	key := appkey.Key{ID: 1, Hash: "first"}
	r := startNextAfterReload(t, key, "telegram.api_hash",
		"telegram:\n  api_id: 1\n  api_hash: first\n",
		"telegram:\n  api_id: 1\n  api_hash: second\n")

	assert.Equal(t, key, r.endpoint.Key)
}

func TestTakesHold_telegram_session_file(t *testing.T) {
	dir := t.TempDir()
	r := startNextAfterReload(t, startupKey, "telegram.session_file",
		"telegram:\n  session_file: "+filepath.Join(dir, "first.json")+"\n",
		"telegram:\n  session_file: "+filepath.Join(dir, "second.json")+"\n")

	assert.Equal(t, filepath.Join(dir, "first.json"), r.endpoint.SessionFile)
}

func TestTakesHold_state_dir(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	r := startNextAfterReload(t, startupKey, "state_dir",
		"state_dir: "+first+"\n",
		"state_dir: "+second+"\n")

	started, err := filepath.EvalSymlinks(first)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(started, "state.db"), r.firstDatabase)
	assert.Equal(t, r.firstDatabase, databaseFile(t, r.next),
		"the next account opened its database outside the directory this process holds")
}

// proxyBefore is a route every proxy test starts on.
const proxyBefore = "proxy:\n  type: socks5\n  server: 127.0.0.1\n  port: 1080\n  username: ada\n  password: first\n"

func TestTakesHold_proxy_type(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "proxy.type", proxyBefore, "proxy:\n  type: direct\n")

	assert.Equal(t, r.first.startup.route, r.next.startup.route)
}

func TestTakesHold_proxy_server(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "proxy.server", proxyBefore,
		"proxy:\n  type: socks5\n  server: 127.0.0.2\n  port: 1080\n  username: ada\n  password: first\n")

	assert.Equal(t, r.first.startup.route, r.next.startup.route)
}

func TestTakesHold_proxy_port(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "proxy.port", proxyBefore,
		"proxy:\n  type: socks5\n  server: 127.0.0.1\n  port: 1081\n  username: ada\n  password: first\n")

	assert.Equal(t, r.first.startup.route, r.next.startup.route)
}

func TestTakesHold_proxy_username(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "proxy.username", proxyBefore,
		"proxy:\n  type: socks5\n  server: 127.0.0.1\n  port: 1080\n  username: grace\n  password: first\n")

	assert.Equal(t, r.first.startup.route, r.next.startup.route)
}

func TestTakesHold_proxy_password(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "proxy.password", proxyBefore,
		"proxy:\n  type: socks5\n  server: 127.0.0.1\n  port: 1080\n  username: ada\n  password: second\n")

	assert.Equal(t, r.first.startup.route, r.next.startup.route)
}

func TestTakesHold_proxy_secret(t *testing.T) {
	const mtproto = "proxy:\n  type: mtproto\n  server: 127.0.0.1\n  port: 1443\n  secret: "
	r := startNextAfterReload(t, startupKey, "proxy.secret",
		mtproto+"0123456789abcdef0123456789abcdef\n",
		mtproto+"fedcba9876543210fedcba9876543210\n")

	assert.Equal(t, r.first.startup.route, r.next.startup.route)
}

func TestTakesHold_photos_mode(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "photos.mode", "photos:\n  mode: kitty\n", "photos:\n  mode: blocks\n")

	assert.Equal(t, media.ModeKitty, r.next.startup.imageMode)
}

func TestTakesHold_photos_disk_cache_size(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "photos.disk_cache_size",
		"photos:\n  disk_cache_size: 1048576\n", "photos:\n  disk_cache_size: 2097152\n")

	assert.Equal(t, int64(1048576), r.next.startup.mediaCacheSize)
}

func TestTakesHold_avatars_disk_cache_size(t *testing.T) {
	r := startNextAfterReload(t, startupKey, "avatars.disk_cache_size",
		"avatars:\n  disk_cache_size: 1048576\n", "avatars:\n  disk_cache_size: 2097152\n")

	assert.Equal(t, int64(1048576), r.next.startup.avatarCacheSize)
}
