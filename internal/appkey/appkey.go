// Package appkey holds the Telegram app key that a build from published source
// runs on: homebrew-core, the Nix flake, a BSD port, anyone's own `go build`.
//
// It is the last of three steps. A key in the person's config wins over
// everything; an official build then uses the one the release pipeline injects
// through ldflags; only a build with neither reaches this package. The key here
// is therefore the same for everyone who compiles tele themselves, which is
// what lets `brew install tele` reach the login screen instead of exiting with
// instructions to go register an application first.
//
// An app key is the id and the hash together. Published hands over both or
// neither: half of one key and half of another authenticates nothing.
package appkey

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// Key is an app key: the api_id and the api_hash, which are one thing and never
// two.
type Key struct {
	ID   int
	Hash string
}

// whole reports that both halves are there.
func (k Key) whole() bool { return k.ID != 0 && k.Hash != "" }

// empty reports that neither half is there.
func (k Key) empty() bool { return k.ID == 0 && k.Hash == "" }

// Source is where the key in force came from.
type Source string

const (
	// SourceUser is a key the person put in their config.
	SourceUser Source = "user"
	// SourceInjected is the key the release pipeline compiled into an official
	// build.
	SourceInjected Source = "injected"
	// SourcePublished is the key in the source tree, which every build from
	// source carries.
	SourcePublished Source = "published"
)

var (
	// ErrHalfKey is a config that names one half of a key and not the other.
	ErrHalfKey = errors.New("the config names only one of api_id and api_hash")
	// ErrNoKey is a build that carries no key, run with a config that names
	// none.
	ErrNoKey = errors.New("no app key")
)

// Resolve picks the key tele runs on: the person's own, then the one injected
// into an official build, then the published one. Each is taken whole or not at
// all, so the key in force is never the id of one and the hash of another.
//
// Half a key in the config is refused rather than passed over. The person
// meant to run on a key of their own, and starting on a built-in one instead
// would be not doing what they asked without saying so.
func Resolve(user, injected Key) (Key, Source, error) {
	switch {
	case user.whole():
		return user, SourceUser, nil
	case !user.empty():
		return Key{}, "", ErrHalfKey
	case injected.whole():
		return injected, SourceInjected, nil
	}
	if id, hash := Published(); id != 0 && hash != "" {
		return Key{ID: id, Hash: hash}, SourcePublished, nil
	}
	return Key{}, "", ErrNoKey
}

// The two halves of the key, each kept in fragments and put back together only
// in Published.
var (
	idFragments   = []string{"MzQy", "NTA2", "NDc="}
	hashFragments = []string{"NDUzNGJhZ", "DI2OGMxZD", "I5OGUwMDZ", "jY2IzYzA4", "YWJlYjU="}
)

// Published returns the app key for builds from source: the api_id and the
// api_hash, in that order.
//
// It returns 0 and "" if the fragments cannot be put back together, which
// leaves the caller on the path it would have taken before this package
// existed - telling the person to supply their own key - rather than sending a
// nonsense one to Telegram.
func Published() (int, string) {
	hash := join(hashFragments)
	if hash == "" {
		return 0, ""
	}
	id, err := strconv.Atoi(join(idFragments))
	if err != nil || id == 0 {
		return 0, ""
	}
	return id, hash
}

// join concatenates the fragments and decodes the result.
func join(fragments []string) string {
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(fragments, ""))
	if err != nil {
		return ""
	}
	return string(decoded)
}
