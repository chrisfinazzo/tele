package core

import (
	"time"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/core/state"
)

// projectionReader is what every projection is built from: the store plus the
// send queue. Asserted here so a change to either interface fails at compile
// time rather than at wiring time.
var _ project.Reader = projectionReader{}

// Deltas is the stream every attached client consumes. Raw state changes do not
// reach a client: a client sees only the projections it subscribed to.
func (o *Owner) Deltas() <-chan project.Delta { return o.deltas }

// Subscribe registers a window. The subscription's first delta carries its
// current contents, which is what makes a resubscribe a full resync.
func (o *Owner) Subscribe(w project.Window) project.SubID {
	// Bring the chat's persisted tail into memory before the window is built, so
	// it paints cached history at once instead of waiting on the network — and
	// still shows something when there is no network at all (#139).
	if cw, ok := w.(project.HistoryWindow); ok {
		o.state.Store().LoadMessages(cw.ChatID)
	}
	id := o.registry.Subscribe(w)
	o.maybeFetch(id, w)
	return id
}

// MoveWindow repositions a subscription. It returns immediately: over a socket a
// window move cannot be synchronous, so it is not synchronous here either.
func (o *Owner) MoveWindow(id project.SubID, w project.Window) {
	o.registry.MoveWindow(id, w)
	o.maybeFetch(id, w)
}

func (o *Owner) Unsubscribe(id project.SubID) { o.registry.Unsubscribe(id) }

// Refresh rebuilds every subscription against current state.
//
// TRANSITIONAL (#193, #195, #196): media still writes to the store directly and
// asks for a rebuild. Commands no longer do — they mutate through state, whose
// commit publishes. The forward preview bump is the one caller inside the owner.
func (o *Owner) Refresh() { o.registry.Refresh() }

// maybeFetch goes to Telegram when the store cannot answer a chat window on its
// own, so a client never has to know where data comes from. There are two
// reasons: the chat has a recorded gap, which is a hole somebody has to close,
// and the window came back short, which is history nobody has fetched yet.
func (o *Owner) maybeFetch(id project.SubID, w project.Window) {
	cw, ok := w.(project.HistoryWindow)
	if !ok || o.client == nil {
		return
	}
	_, hasGap := o.state.Store().Gap(cw.ChatID)
	if !hasGap && !needsBackfill(project.BuildHistory(o.reader(), cw), cw) {
		return
	}
	go o.fill(o.ctx, id, cw)
}

// needsBackfill reports that the store could not fill the window: it returned
// fewer messages than were asked for and has nothing older left to give. This is
// deliberately not HasOlder, which reports the opposite — that the store does
// hold more, so no fetch is needed.
func needsBackfill(c project.HistoryContents, w project.HistoryWindow) bool {
	return !c.HasOlder && len(c.Messages) < w.Before+w.After+1
}

// publishChange turns one applied domain change into whatever the current
// subscriptions need to hear. Typing goes out as an event instead: it has no
// persisted state to rebuild a projection from, and nothing would ever clear it
// if it were held as one.
func (o *Owner) publishChange(chg state.Change) {
	if chg.Kind == state.ChangeTyping {
		o.publishTyping(Typing{ChatID: chg.ChatID, Label: chg.Typing.Label()})
		return
	}
	// A message arriving may be one this owner queued. Dropping the entry here,
	// before the rebuild, is what makes the pending bubble and the real message
	// swap inside a single delta rather than across two, with a frame showing
	// neither in between (#193).
	if chg.Kind == state.ChangeNewMessage {
		o.clearSentOutbox(chg.ChatID)
	}
	o.registry.Refresh()
}

// resyncDelay is how long after a dropped delta the owner rebuilds on its own.
// Long enough for a stalled client to drain, short enough that the repair is
// not noticed.
const resyncDelay = 200 * time.Millisecond

// emit drops a delta rather than blocking when a client is not draining:
// backpressure must never stall the owner's update loop. Reporting the drop is
// what repairs it: the registry forgets what that subscription was told, so its
// next delta is a resync rather than one stated against a delta never received.
//
// That next delta must not wait for an unrelated change, so a drop also
// schedules one rebuild. It runs on a timer of its own: emit is called under
// the registry's lock and must not call back into it.
func (o *Owner) emit(d project.Delta) bool {
	select {
	case o.deltas <- d:
		return true
	default:
		o.log.Warn("projection delta dropped: client is not draining")
		if o.resyncArmed.CompareAndSwap(false, true) {
			time.AfterFunc(resyncDelay, func() {
				// Disarmed before the rebuild, so a drop during it schedules
				// another rather than being lost.
				o.resyncArmed.Store(false)
				o.Refresh()
			})
		}
		return false
	}
}
