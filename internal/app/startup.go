package app

import (
	"os"

	"github.com/sorokin-vladimir/tele/internal/appkey"
	"github.com/sorokin-vladimir/tele/internal/config"
	"github.com/sorokin-vladimir/tele/internal/proxy"
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
	route         proxy.Route
	// The cache budgets, in bytes. Zero keeps nothing between runs.
	mediaCacheSize  int64
	avatarCacheSize int64
	imageMode       media.Mode
}

// newStartup reads the startup values out of the config the process started
// with, beside the app key it resolved.
func newStartup(cfg *config.Config, key appkey.Key) (startup, error) {
	// The route was already accepted when the config loaded. It is parsed
	// again here rather than carried on Config, because a Route is cheap and a
	// second field that has to be kept in step with the section is not.
	route, err := proxy.Parse(cfg.Proxy)
	if err != nil {
		return startup{}, err
	}
	return startup{
		key:             key,
		stateDir:        cfg.StateDir,
		sessionFile:     cfg.Telegram.SessionFile,
		sessionPinned:   cfg.SessionPinned,
		route:           route,
		mediaCacheSize:  cfg.Photos.DiskCacheSize,
		avatarCacheSize: cfg.Avatars.DiskCacheSize,
		imageMode:       media.DetectMode(cfg.Photos.Mode, os.Getenv),
	}, nil
}

// endpoint is what a connection is made with. Each connection is given a
// resolver of its own, built from the one route the process started with.
func (s startup) endpoint() (internaltg.Endpoint, error) {
	resolver, err := proxy.Resolver(s.route)
	if err != nil {
		return internaltg.Endpoint{}, err
	}
	return internaltg.Endpoint{Key: s.key, SessionFile: s.sessionFile, Resolver: resolver}, nil
}
