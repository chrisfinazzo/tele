package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/sorokin-vladimir/tele/internal/config"
	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/core/state"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

type nopNotifier struct{}

func (nopNotifier) Notify(string, string) error { return nil }

// newTestOwner builds an owner over an in-memory store with no Telegram client:
// the update loop is fed directly through the returned channel.
func newTestOwner(t *testing.T) (*Owner, chan store.Event, store.Store) {
	t.Helper()
	st := store.NewMemory()
	s := state.New(st)
	events := make(chan store.Event, 8)
	o := New(&config.Config{}, zap.NewNop(), s, nil, nopNotifier{})
	o.events = events
	return o, events, st
}

// newTestOwnerNotified is newTestOwner with a notifier the test can inspect,
// message previews on so a body is a body rather than "New message", and both
// sinks on. A zero Config would have every switch off, which is the opposite of
// what a fresh install does.
func newTestOwnerNotified(t *testing.T, n Notifier) (*Owner, store.Store) {
	t.Helper()
	st := store.NewMemory()
	return New(notifyConfig(true, true), zap.NewNop(), state.New(st), nil, n), st
}

// notifyConfig is a config with previews on and the two sinks as asked, so a
// test names only the switch it is about.
func notifyConfig(desktop, toast bool) *config.Config {
	cfg := &config.Config{}
	cfg.UI.Notifications.Preview = true
	cfg.UI.Notifications.Desktop = desktop
	cfg.UI.Notifications.Toast = toast
	return cfg
}

func recvDelta(t *testing.T, ch <-chan project.Delta) (project.Delta, bool) {
	t.Helper()
	select {
	case d := <-ch:
		return d, true
	case <-time.After(time.Second):
		return project.Delta{}, false
	}
}

func TestUpdateLoop_AppliesAndPublishes(t *testing.T) {
	o, events, st := newTestOwner(t)
	st.SetChat(domain.Chat{ID: 1, Title: "A"})
	o.Subscribe(project.ChatListWindow{Limit: 10})
	_, _ = recvDelta(t, o.Deltas()) // the subscription's initial Reset
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.RunUpdates(ctx)

	events <- store.Event{Kind: store.EventNewMessage, Message: domain.Message{ID: 5, ChatID: 1, Text: "hi"}}

	d, got := recvDelta(t, o.Deltas())
	require.True(t, got, "the owner must publish a delta")
	require.NotNil(t, d.ChatList)
	assert.Equal(t, 1, d.ChatList.Row.Unread)

	c, _ := st.GetChat(1)
	assert.Equal(t, 1, c.UnreadCount, "the owner applied the mutation, not the client")
}

// A read receipt that does not advance the pointer changes nothing, so no client
// is woken.
func TestUpdateLoop_NoopEventPublishesNothing(t *testing.T) {
	o, events, st := newTestOwner(t)
	st.SetChat(domain.Chat{ID: 1, ReadInboxMaxID: 10, UnreadCount: 3})
	o.Subscribe(project.ChatListWindow{Limit: 10})
	_, _ = recvDelta(t, o.Deltas())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.RunUpdates(ctx)

	events <- store.Event{Kind: store.EventReadInbox, ChatID: 1, ReadMaxID: 10}
	// Follow it with an event that does produce a change, so the assertion does
	// not merely observe a slow loop.
	events <- store.Event{Kind: store.EventReadInbox, ChatID: 1, ReadMaxID: 20}

	d, got := recvDelta(t, o.Deltas())
	require.True(t, got)
	require.NotNil(t, d.ChatList)
	assert.Equal(t, project.ChatListRow, d.ChatList.Kind)
	assert.Zero(t, d.ChatList.Row.Unread, "the advancing receipt cleared the count")

	_, more := recvDelta(t, o.Deltas())
	assert.False(t, more, "the no-op receipt must not have produced a delta")
}

// A client that subscribes to a window nowhere near a change hears nothing. This
// is the presence-storm fix: presence streams for every online contact.
func TestUpdateLoop_ChangeOutsideEveryWindowPublishesNothing(t *testing.T) {
	o, events, st := newTestOwner(t)
	// Distinct last-message times: chats with equal sort keys come out of the
	// store in map order, which would make "is chat 3 in the window" a coin toss.
	for i := 1; i <= 30; i++ {
		st.SetChat(domain.Chat{
			ID:          int64(i),
			Title:       "chat",
			Peer:        domain.Peer{ID: int64(i)},
			LastMessage: &domain.Message{ID: i, Date: time.Unix(int64(1000-i), 0)},
		})
	}
	o.Subscribe(project.ChatListWindow{Offset: 0, Limit: 5})
	_, _ = recvDelta(t, o.Deltas())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.RunUpdates(ctx)

	events <- store.Event{Kind: store.EventUserPresence, ChatID: 25, Online: true}
	events <- store.Event{Kind: store.EventUserPresence, ChatID: 3, Online: true}

	d, got := recvDelta(t, o.Deltas())
	require.True(t, got)
	require.NotNil(t, d.ChatList)
	assert.Equal(t, int64(3), d.ChatList.Row.ID, "only the in-window chat produced a delta")

	_, more := recvDelta(t, o.Deltas())
	assert.False(t, more)
}

func TestUpdateLoop_StopsOnContextCancel(t *testing.T) {
	o, _, _ := newTestOwner(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { o.RunUpdates(ctx); close(done) }()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("update loop did not stop on context cancellation")
	}
}

// The whole point of #192: the banner and the toast are one value, so they
// cannot disagree about the same event.
func TestNotify_OneEventFeedsBothSinks(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})

	o.handleEvent(store.Event{Kind: store.EventNewMessage,
		Message: domain.Message{ChatID: 2, Text: "hello there", Date: time.Now()}})

	require.Len(t, n.calls, 1, "exactly one OS notification")
	select {
	case got := <-o.Notifications():
		assert.Equal(t, n.calls[0].title, got.Title, "same value, two sinks")
		assert.Equal(t, n.calls[0].body, got.Body)
		assert.Equal(t, int64(2), got.ChatID)
	default:
		t.Fatal("expected a Notification on the stream")
	}
}

// Delivery is at least once, so the wire and a recovered difference can both
// carry the same message. Only the first is an arrival: the second must ring
// nothing and flash nothing (ADR 0016).
func TestNotify_SecondCopyOfAMessageIsSilent(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})
	evt := store.Event{Kind: store.EventNewMessage,
		Message: domain.Message{ID: 40, ChatID: 2, Text: "hello there", Date: time.Now()}}

	o.handleEvent(evt)
	o.handleEvent(evt)

	assert.Len(t, n.calls, 1, "one arrival, one banner")
	assert.Equal(t, 1, len(o.Notifications()), "one toast")
	assert.Equal(t, 1, len(o.Incoming()), "one row flash")
}

// The two gates are different on purpose: mute silences the interruption but
// must not suppress the row flash that follows the reorder (#39).
func TestNotify_MutedChatStillFlashesTheRow(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	st.SetChat(domain.Chat{ID: 2, Title: "Bob", IsMuted: true})

	o.handleEvent(store.Event{Kind: store.EventNewMessage,
		Message: domain.Message{ChatID: 2, Text: "hey", Date: time.Now()}})

	assert.Empty(t, n.calls, "a muted chat raises no banner")
	select {
	case got := <-o.Notifications():
		t.Fatalf("a muted chat must raise no toast either, got %+v", got)
	default:
	}
	select {
	case in := <-o.Incoming():
		assert.Equal(t, int64(2), in.ChatID, "the row still flashes")
	default:
		t.Fatal("expected an Incoming for the row flash")
	}
}

// Each sink is switched on its own. The decision is made either way: what the
// switch says is where a notification goes, never whether it was decided
// (#249, ADR 0013).
func TestNotify_DesktopSinkOffLeavesTheToast(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	o.SetConfig(notifyConfig(false, true))
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})

	o.handleEvent(store.Event{Kind: store.EventNewMessage,
		Message: domain.Message{ChatID: 2, Text: "hello there", Date: time.Now()}})

	assert.Empty(t, n.calls, "nothing reaches the operating system")
	select {
	case got := <-o.Notifications():
		assert.Equal(t, "hello there", got.Body, "and the toast is unchanged")
	default:
		t.Fatal("expected a Notification on the stream")
	}
}

func TestNotify_ToastSinkOffLeavesTheDesktopNotification(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	o.SetConfig(notifyConfig(true, false))
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})

	o.handleEvent(store.Event{Kind: store.EventNewMessage,
		Message: domain.Message{ChatID: 2, Text: "hello there", Date: time.Now()}})

	require.Len(t, n.calls, 1, "the desktop notification is unchanged")
	assert.Equal(t, "hello there", n.calls[0].body)
	select {
	case got := <-o.Notifications():
		t.Fatalf("no client should be sent a toast, got %+v", got)
	default:
	}
}

// Both off is a legal state and the whole point of #249. The chat list is not a
// sink: the row still flashes, exactly as it does for a muted chat.
func TestNotify_BothSinksOffStillFlashesTheRow(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	o.SetConfig(notifyConfig(false, false))
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})

	o.handleEvent(store.Event{Kind: store.EventNewMessage,
		Message: domain.Message{ChatID: 2, Text: "hey", Date: time.Now()}})

	assert.Empty(t, n.calls, "no desktop notification")
	select {
	case got := <-o.Notifications():
		t.Fatalf("no toast either, got %+v", got)
	default:
	}
	select {
	case in := <-o.Incoming():
		assert.Equal(t, int64(2), in.ChatID, "the row still flashes")
	default:
		t.Fatal("expected an Incoming for the row flash")
	}
}

func TestNotify_FocusedChatDoesNeither(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})
	o.Attach().SetFocus(domain.HistoryKey{ChatID: 2})

	o.handleEvent(store.Event{Kind: store.EventNewMessage,
		Message: domain.Message{ChatID: 2, Text: "hey", Date: time.Now()}})

	assert.Empty(t, n.calls)
	select {
	case <-o.Notifications():
		t.Fatal("the chat on screen must not toast")
	default:
	}
	select {
	case <-o.Incoming():
		t.Fatal("the chat on screen must not flash")
	default:
	}
}

// A group reaction reaches the client as a decision like any other. The client
// renders a Notification without knowing what kind of event produced it, which
// is what #192 bought and what #203's toast requirement amounts to.
func TestNotify_GroupReactionFeedsBothSinks(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})

	o.handleEvent(store.Event{
		Kind: store.EventReactionsUpdate, ChatID: 2, MsgID: 10,
		ReactionsUnread: true, ReactionEmoji: "❤", ReactionDate: time.Now(),
	})

	require.Len(t, n.calls, 1, "exactly one OS notification")
	assert.Equal(t, "reacted ❤ to your message", n.calls[0].body)
	select {
	case got := <-o.Notifications():
		assert.Equal(t, n.calls[0].title, got.Title, "same value, two sinks")
		assert.Equal(t, n.calls[0].body, got.Body)
		assert.Equal(t, int64(2), got.ChatID, "the toast opens this chat when clicked")
	default:
		t.Fatal("expected a Notification on the stream")
	}
	// A reaction does not reorder the chat list, so there is nothing for the eye
	// to follow and no row flash to publish (#203). The unread-reactions badge is
	// the standing cue; the toast is the interruption.
	select {
	case in := <-o.Incoming():
		t.Fatalf("a reaction must not flash the row, got %+v", in)
	default:
	}
}

// The 1:1 form: Telegram delivers a peer's reaction as a hidden edit carrying
// unread reactions, not as a reactions update.
func TestNotify_DMReactionFeedsBothSinks(t *testing.T) {
	n := &mockNotifier{}
	o, st := newTestOwnerNotified(t, n)
	st.SetChat(domain.Chat{ID: 2, Title: "Bob"})

	o.handleEvent(store.Event{
		Kind: store.EventEditMessage,
		Message: domain.Message{
			ChatID: 2, ID: 10, IsOut: true, HasUnreadReactions: true,
		},
		ReactionEmoji: "👍", ReactionDate: time.Now(),
	})

	require.Len(t, n.calls, 1, "exactly one OS notification")
	assert.Equal(t, "reacted 👍 to your message", n.calls[0].body)
	select {
	case got := <-o.Notifications():
		assert.Equal(t, n.calls[0].title, got.Title, "same value, two sinks")
		assert.Equal(t, n.calls[0].body, got.Body)
		assert.Equal(t, int64(2), got.ChatID)
	default:
		t.Fatal("expected a Notification on the stream")
	}
	select {
	case in := <-o.Incoming():
		t.Fatalf("a reaction must not flash the row, got %+v", in)
	default:
	}
}
