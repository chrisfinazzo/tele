package app

import (
	"os"

	"github.com/sorokin-vladimir/tele/internal/appkey"
	"github.com/sorokin-vladimir/tele/internal/config"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
	"github.com/sorokin-vladimir/tele/internal/ui/media"
)

// startup is what the process settles once, when it starts, and hands every
// account it opens: the startup settings and what is derived from them (#239).
//
// The config is what the file says now; a reload replaces it, and an account
// that starts after a log out would otherwise open from whatever the file says
// by then - its database in a state directory this process holds no lock on,
// its connection with no app key, since the key a build carries was never in
// the file. An account takes its live settings from the config and these from
// here.
type startup struct {
	key           appkey.Key
	stateDir      string
	sessionFile   string
	sessionPinned bool
	// The cache budgets, in bytes. Zero keeps nothing between runs.
	mediaCacheSize  int64
	avatarCacheSize int64
	imageMode       media.Mode
}

// newStartup reads the startup values out of the config the process started
// with, beside the app key it resolved.
func newStartup(cfg *config.Config, key appkey.Key) startup {
	return startup{
		key:             key,
		stateDir:        cfg.StateDir,
		sessionFile:     cfg.Telegram.SessionFile,
		sessionPinned:   cfg.SessionPinned,
		mediaCacheSize:  cfg.Photos.DiskCacheSize,
		avatarCacheSize: cfg.Avatars.DiskCacheSize,
		imageMode:       media.DetectMode(cfg.Photos.Mode, os.Getenv),
	}
}

// endpoint is what each connection is made with.
func (s startup) endpoint() internaltg.Endpoint {
	return internaltg.Endpoint{Key: s.key, SessionFile: s.sessionFile}
}
