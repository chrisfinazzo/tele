package core

import (
	"github.com/sorokin-vladimir/tele/internal/store"
)

// Incoming reports that something arrived in a chat the client is not showing:
// enough for the row to flash and follow the reorder (#39). It is an event, not
// state, which is why it does not ride on a projection — a chat-list row says
// what the unread count is now, never that something just happened.
//
// Whether it also deserves the user's attention is a Notification, decided
// separately. The gates differ: a muted chat still flashes.
type Incoming struct {
	ChatID int64
}

// Failure reports work the owner could not finish on a client's behalf. A client
// cannot see the attempt — filling a window may or may not touch the network —
// so a silent failure would leave a spinner running forever.
//
// Err carries the domain error kind from #191; the client decides how to say it.
type Failure struct {
	ChatID int64
	Op     string
	Err    error
}

// OpSend names a send the queue gave up on. Clients single it out: a refused
// send is not a window that failed to fill, so it belongs in a toast rather
// than in the pane's load-error banner, and the message is still on screen to
// retry (#193).
const OpSend = "send"

// Typing reports that someone started or stopped composing in a chat. An empty
// Label means stopped.
//
// It is an event rather than part of the chat projection, because there is no
// state behind it to be right about: Telegram is not obliged to send a stop, so
// a stored label has nothing to clear it and would outlive the typing forever.
// The client shows it and lets it expire.
type Typing struct {
	ChatID int64
	// TopicID is the forum topic the typing is in, 0 outside a forum (#275).
	TopicID int
	Label   string
}

// Incoming is the event stream a client renders alongside its projections.
func (o *Owner) Incoming() <-chan Incoming { return o.incoming }

// Typing reports composing activity in the chats a client may be showing.
func (o *Owner) Typing() <-chan Typing { return o.typing }

func (o *Owner) publishTyping(t Typing) {
	select {
	case o.typing <- t:
	default:
		o.log.Warn("typing event dropped: client is not draining")
	}
}

// Progress is how far a queued media send has uploaded.
//
// It is an event rather than part of the chat projection, for the reason typing
// is: there is nothing persisted behind it, a dropped frame is corrected by the
// next one, and the uploader emits one per chunk. Routed through the projection,
// that stream would rebuild and diff every subscription per chunk, and would
// compete for the delta buffer with changes whose loss costs a stale window.
type Progress struct {
	ChatID int64
	Ref    string
	// Part is the 1-based file being uploaded now, Parts the total in the entry.
	Part  int
	Parts int
	// Done and Total are bytes over the whole entry, so a client renders the
	// aggregate without having to know what each part weighs.
	Done  int64
	Total int64
}

// Progress reports upload advance for the queued sends a client may be showing.
func (o *Owner) Progress() <-chan Progress { return o.progress }

// DownloadProgress is how far a save the client asked to watch has come. It is
// addressed to the call rather than to every client: Ref is whatever the caller
// passed to SaveMedia, and over a socket it is the call's own id. Like upload
// progress it is an absolute value, so a frame lost on the way is corrected by
// the next one (#204).
type DownloadProgress struct {
	Ref   string
	Done  int64
	Total int64
}

// Downloads reports progress for saves that were given a ref.
func (o *Owner) Downloads() <-chan DownloadProgress { return o.downloads }

// publishDownload drops rather than blocks, and quietly, for the reason
// publishProgress does.
func (o *Owner) publishDownload(p DownloadProgress) {
	select {
	case o.downloads <- p:
	default:
	}
}

// publishProgress drops rather than blocks. Unlike typing it does not log the
// drop: under a large upload that would be thousands of lines about a repaint
// nobody missed.
func (o *Owner) publishProgress(p Progress) {
	select {
	case o.progress <- p:
	default:
	}
}

// Failures reports operations the owner could not complete.
func (o *Owner) Failures() <-chan Failure { return o.failures }

func (o *Owner) publishFailure(f Failure) {
	select {
	case o.failures <- f:
	default:
		o.log.Warn("failure event dropped: client is not draining")
	}
}

// Notifications is the stream of decisions the owner has already made. A client
// renders them; it never re-judges (#192).
func (o *Owner) Notifications() <-chan Notification { return o.notifications }

// publishNotification drops rather than blocks: a client that stopped draining
// must never stall the update loop, and a client that has stopped draining is
// not drawing the toast anyway. It used to be worth saying that the desktop
// notification carried the same value regardless; that sink can now be switched
// off (#249), so the consolation is gone and the reason is the stall alone.
func (o *Owner) publishNotification(n Notification) {
	select {
	case o.notifications <- n:
	default:
		o.log.Warn("notification dropped: client is not draining")
	}
}

// publishIncoming reports an inbound message in a chat no client is showing.
// Outgoing messages and the focused chat are excluded: neither should flash a
// row. Mute is deliberately not consulted — that gates the interruption, not the
// reorder cue.
func (o *Owner) publishIncoming(evt store.Event, focused func(int64) bool) {
	if evt.Kind != store.EventNewMessage || evt.Message.IsOut {
		return
	}
	if focused(evt.Message.ChatID) {
		return
	}
	if _, ok := o.state.Store().GetChat(evt.Message.ChatID); !ok {
		return
	}
	select {
	case o.incoming <- Incoming{ChatID: evt.Message.ChatID}:
	default:
		o.log.Warn("incoming event dropped: client is not draining")
	}
}
