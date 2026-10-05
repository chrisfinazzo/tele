// Package telerr defines the closed set of error kinds that may leave the
// Telegram layer. Mapping gotd errors onto it happens exactly once, in
// internal/tg; nothing outside that package inspects gotd error values.
//
// This package deliberately imports nothing but the standard library: in v2 it
// is shared by the owner process and by clients that never link gotd at all.
package telerr

import (
	"errors"
	"time"
)

// Kind classifies a failure. The set is closed, so callers switch over it
// exhaustively rather than matching sentinels.
type Kind string

const (
	// Unauthorized means the account was logged out: by the person, from
	// another device, or by Telegram, including when the Telegram account
	// behind it was deleted. Logging in again is the remedy.
	Unauthorized Kind = "unauthorized"
	// AccountBanned means Telegram closed the account for good, together with
	// the phone number behind it. Unlike Unauthorized, logging in again meets
	// the same refusal, so it is terminal and a login is never offered for it.
	AccountBanned Kind = "account_banned"
	// AppKeyBlocked means Telegram refused the app key this binary runs on,
	// rather than the session it holds. Signing in again cannot help: the same
	// key is offered every time. The remedy is a different key - the person's
	// own in the config, or a build carrying another one - which makes this
	// terminal wherever Unauthorized would be worth waiting out.
	AppKeyBlocked Kind = "app_key_blocked"
	// RateLimited means Telegram asked us to wait. Carries RetryAfter.
	RateLimited Kind = "rate_limited"
	// PeerNotFound means the peer is unknown or inaccessible.
	PeerNotFound Kind = "peer_not_found"
	// Forbidden means the action is not allowed here: restriction, ban, slowmode.
	Forbidden Kind = "forbidden"
	// NotFound means the message or media is gone.
	NotFound Kind = "not_found"
	// StaleReference means the file reference we hold expired. Re-fetch it and
	// retry; the media itself still exists.
	StaleReference Kind = "stale_reference"
	// Network means a transport failure. Carries Transient.
	Network Kind = "network"
	// Rejected means Telegram understood the request and refused the content:
	// a file it will not take, a text it will not carry. Terminal by nature —
	// the same content will be refused again — so it is the person's to resolve
	// and never a timer's. Carries Reason.
	Rejected Kind = "rejected"
	// Internal means a bug or an unmapped condition.
	Internal Kind = "internal"
)

// Reason refines Rejected: what about the content was refused. The set is
// closed, like Kind, so that the decision of what to tell a person is made
// against a fixed list rather than by matching Telegram's error types — those
// are mapped exactly once, in internal/tg, and nothing outside it should have to
// know them. A Reason is deliberately coarser than the types behind it: several
// types that call for the same remedy share one.
type Reason string

const (
	// ReasonPhotoType means the file is not in a form Telegram accepts as a
	// photo, usually because its name carries no usable extension.
	ReasonPhotoType Reason = "photo_type"
	// ReasonPhotoDimensions means the image is too large or too oddly shaped.
	ReasonPhotoDimensions Reason = "photo_dimensions"
	// ReasonMediaUnreadable means Telegram took the bytes and could not make
	// sense of them: a truncated image, a video it cannot process.
	ReasonMediaUnreadable Reason = "media_unreadable"
	// ReasonMediaUnsupported means the attachment is not something this chat or
	// this account can carry.
	ReasonMediaUnsupported Reason = "media_unsupported"
	// ReasonTextEmpty means there was nothing to send.
	ReasonTextEmpty Reason = "text_empty"
	// ReasonTextTooLong means the message is over Telegram's length limit.
	ReasonTextTooLong Reason = "text_too_long"
	// ReasonMarkupTooLong means the formatting, rather than the text, is what
	// exceeds a limit.
	ReasonMarkupTooLong Reason = "markup_too_long"
)

// Error is the only error shape that leaves internal/tg.
//
// The fields are exported and Kind is a string because in v2 this value is the
// payload of the IPC error frame, with no second representation.
type Error struct {
	Kind Kind `json:"kind"`
	// Op names the failed operation, usually the TL method, e.g.
	// "messages.sendMessage".
	Op string `json:"op,omitempty"`
	// Detail carries the raw Telegram error type for logs, and is the only
	// thing worth showing a user when Kind is Internal.
	Detail string `json:"detail,omitempty"`
	// Reason is set for Rejected only, and is what a client says to a person.
	// Detail stays alongside it for the logs: the Reason is the remedy, the
	// Detail is the evidence.
	Reason Reason `json:"reason,omitempty"`
	// RetryAfter is set for RateLimited only.
	RetryAfter time.Duration `json:"retry_after,omitempty"`
	// Transient is set for Network only, and reports whether a retry may help.
	Transient bool `json:"transient,omitempty"`
	// SessionRemoved is set for Unauthorized only, when the owner removed the
	// session itself because Telegram had invalidated its key and no login could
	// use it. It reports what the owner did rather than what Telegram said: the
	// next start begins a new account, and a client says so.
	SessionRemoved bool `json:"session_removed,omitempty"`
	// Cause is the underlying error. It is not serialisable and never crosses a
	// process boundary, but it must be preserved in-process: gotd recognises
	// its own errors through the unwrap chain.
	Cause error `json:"-"`
}

func (e *Error) Error() string {
	msg := string(e.Kind)
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	if e.Op != "" {
		return e.Op + ": " + msg
	}
	return msg
}

// Unwrap keeps the original error reachable. gotd matches its own errors with
// tgerr.Is, which is errors.As underneath, so breaking this chain breaks the
// library's internal logic silently.
func (e *Error) Unwrap() error { return e.Cause }

// As reports whether err is a domain error and returns it. Use it when the
// kind alone is not enough, e.g. to read RetryAfter.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Of reports the kind of err, walking the wrap chain. A nil error has no kind;
// anything unrecognised is Internal.
func Of(err error) Kind {
	if err == nil {
		return ""
	}
	if e, ok := As(err); ok {
		return e.Kind
	}
	return Internal
}
