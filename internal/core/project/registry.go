package project

import "sync"

// Emit hands one delta to the client. It reports whether the delta was taken.
//
// It runs under the registry's lock, which is what keeps a subscription's
// deltas in the order they were computed: each is stated against the one
// before it, so two refreshes on different goroutines must not swap theirs on
// the way out. It must therefore never block and never call back into the
// registry.
type Emit func(Delta) bool

// Registry holds the live subscriptions and the contents each was last told
// about. It is the only stateful piece of this package: builders and diffs are
// pure, so everything that can go wrong concentrates here.
type Registry struct {
	mu     sync.Mutex
	reader Reader
	emit   Emit
	next   SubID
	subs   map[SubID]*sub
}

type sub struct {
	window Window
	list   ChatListContents
	chat   ChatContents
}

func NewRegistry(r Reader, emit Emit) *Registry {
	return &Registry{reader: r, emit: emit, subs: make(map[SubID]*sub)}
}

// Subscribe registers a window and replies with its current contents, so a
// resubscribe is a full resync and no snapshot concept is needed.
func (g *Registry) Subscribe(w Window) SubID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	id := g.next
	g.subs[id] = &sub{window: w}
	g.send(g.rebuild(id))
	return id
}

// MoveWindow replaces a subscription's window. It carries a whole window rather
// than a delta, so repeating it is equivalent to resubscribing.
func (g *Registry) MoveWindow(id SubID, w Window) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.subs[id]
	if !ok {
		return
	}
	s.window = carryPin(s.window, w)
	g.send(g.rebuild(id))
}

// carryPin keeps a first-unread window's pinned anchor across a move that does
// not name one. Widening for older history repeats the anchor the client opened
// with, and re-pinning it there would re-anchor the window on whatever happens
// to be unread by then. A client asking for a different chat, or naming an
// anchor itself, gets exactly what it asked for.
func carryPin(prev, next Window) Window {
	p, okPrev := prev.(ChatWindow)
	n, okNext := next.(ChatWindow)
	if !okPrev || !okNext {
		return next
	}
	if p.ChatID != n.ChatID || p.Anchor.Kind != n.Anchor.Kind || n.Anchor.MsgID != 0 {
		return next
	}
	n.Anchor.MsgID = p.Anchor.MsgID
	return n
}

func (g *Registry) Unsubscribe(id SubID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.subs, id)
}

// Window reports a subscription's current window, so the owner can decide
// whether it needs backfilling from Telegram.
func (g *Registry) Window(id SubID) (Window, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.subs[id]
	if !ok {
		return nil, false
	}
	return s.window, true
}

// Refresh rebuilds every subscription and emits whatever actually differs.
// The owner calls it once per applied state change; a change no window contains
// produces no deltas, which is the point.
func (g *Registry) Refresh() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.send(g.rebuildAll())
}

// send emits deltas in the order they were computed. Caller holds the lock.
func (g *Registry) send(ds []Delta) {
	for _, d := range ds {
		g.emit(d)
	}
}

// rebuildAll refreshes every subscription. Caller holds the lock. Map iteration
// makes the order across subscriptions unspecified, which is fine: every delta
// is addressed to one subscription.
func (g *Registry) rebuildAll() []Delta {
	var out []Delta
	for id := range g.subs {
		out = append(out, g.rebuild(id)...)
	}
	return out
}

// rebuild recomputes one subscription's contents and diffs it against what the
// client was last told. Caller holds the lock.
func (g *Registry) rebuild(id SubID) []Delta {
	s := g.subs[id]
	var out []Delta
	switch w := s.window.(type) {
	case ChatListWindow:
		next := BuildChatList(g.reader, w)
		for _, d := range DiffChatList(s.list, next) {
			out = append(out, Delta{Sub: id, ChatList: &d})
		}
		s.list = next
	case ChatWindow:
		next := BuildChat(g.reader, w)
		// Pin a first-unread window to the message it actually opened on. The
		// anchor is otherwise a function of the read pointer, which moves as the
		// user reads: the window would walk forward and drop the messages above
		// out from under them. Pinning happens on the first rebuild that resolves
		// an anchor, so a window opened before its history arrived pins once the
		// backfill lands.
		if w.Anchor.Kind == AnchorFirstUnread && w.Anchor.MsgID == 0 && next.AnchorMsgID != 0 {
			w.Anchor.MsgID = next.AnchorMsgID
			s.window = w
		}
		for _, d := range DiffChat(s.chat, next) {
			out = append(out, Delta{Sub: id, Chat: &d})
		}
		s.chat = next
	}
	return out
}
