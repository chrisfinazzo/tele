package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sorokin-vladimir/tele/internal/core"
	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// fakeAccount is the owner's surface as a command sees it. Only what a test
// sets up answers: any other call panics on the nil Surface (#181).
type fakeAccount struct {
	core.Surface
	stream chan any
	// subs are the windows subscribed, in order; the first gets SubID 1.
	subs  []project.Window
	chats []project.ChatRow
}

func newFakeAccount() *fakeAccount {
	return &fakeAccount{stream: make(chan any, 16)}
}

func (f *fakeAccount) Stream() <-chan any { return f.stream }

func (f *fakeAccount) Subscribe(w project.Window) project.SubID {
	f.subs = append(f.subs, w)
	return project.SubID(len(f.subs))
}

func (f *fakeAccount) Chats(context.Context) ([]project.ChatRow, error) { return f.chats, nil }

// say puts the account in phase on the stream, addressed to subscription sub.
func (f *fakeAccount) say(sub project.SubID, phase project.AccountPhase) {
	f.stream <- project.Delta{Sub: sub, Account: &project.AccountDelta{Contents: project.AccountContents{Phase: phase}}}
}

func shortCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// An account waiting on a login is not logged in: the command does not wait
// for a person to log in, it exits 4 and says where to do it (#181).
func TestAwaitReady_AnAccountWaitingOnALoginExitsFour(t *testing.T) {
	acc := newFakeAccount()
	acc.say(1, project.AccountConnecting)
	acc.say(1, project.AccountLoggingIn)

	err := awaitReady(shortCtx(t), acc)

	var code *exitCode
	if !errors.As(err, &code) || code.code != notLoggedInCode {
		t.Fatalf("got %v, want exit %d", err, notLoggedInCode)
	}
	assertSays(t, code.msg, "not logged in", "tele")
}

// Between one owner and the next there is no account to be ready. A
// subscription ends with its owner, so the command subscribes again and waits
// for the next one (#181).
func TestAwaitReady_WaitsThroughAnAccountEnding(t *testing.T) {
	acc := newFakeAccount()
	acc.say(1, project.AccountLoading)
	acc.stream <- core.AccountEnded{Reason: core.EndLoggedOutElsewhere}
	acc.say(2, project.AccountConnecting)
	acc.say(2, project.AccountReady)

	if err := awaitReady(shortCtx(t), acc); err != nil {
		t.Fatal(err)
	}
	if len(acc.subs) != 2 {
		t.Errorf("subscribed %d times, want once per owner", len(acc.subs))
	}
}

// A host that goes away leaves nobody to ask: the command exits 3, as when
// there was none to begin with (#181).
func TestAwaitReady_AHostGoneExitsThree(t *testing.T) {
	acc := newFakeAccount()
	acc.say(1, project.AccountConnecting)
	close(acc.stream)

	err := awaitReady(shortCtx(t), acc)

	var code *exitCode
	if !errors.As(err, &code) || code.code != noHostCode {
		t.Fatalf("got %v, want exit %d", err, noHostCode)
	}
}

// A connection that ended with anything but a log out will not come back by
// itself: the command says what it ended with instead of waiting it out. A log
// out is waited through, since the next account follows it (#181).
func TestAwaitReady_AConnectionThatEndedIsSaid(t *testing.T) {
	acc := newFakeAccount()
	acc.stream <- project.Delta{Sub: 1, Account: &project.AccountDelta{Contents: project.AccountContents{
		Phase: project.AccountConnecting,
		Ended: &telerr.Error{Kind: telerr.AppKeyBlocked},
	}}}

	err := awaitReady(shortCtx(t), acc)

	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want what the connection ended with", err)
	}
	assertSays(t, err.Error(), "ended", string(telerr.AppKeyBlocked))
}

func TestAwaitReady_ALogOutIsWaitedThrough(t *testing.T) {
	acc := newFakeAccount()
	acc.stream <- project.Delta{Sub: 1, Account: &project.AccountDelta{Contents: project.AccountContents{
		Phase: project.AccountReady,
		Ended: &telerr.Error{Kind: telerr.Unauthorized, LogOut: telerr.LogOutElsewhere},
	}}}
	acc.stream <- core.AccountEnded{Reason: core.EndLoggedOutElsewhere}
	acc.say(2, project.AccountReady)

	if err := awaitReady(shortCtx(t), acc); err != nil {
		t.Fatal(err)
	}
	if len(acc.subs) != 2 {
		t.Errorf("answered from the account that was logged out of")
	}
}

// `tele chats` prints the main list in its order, a line per chat with its id,
// title and badges, and leaves the archived chats out (#181).
func TestChats_ListsTheMainListWithBadges(t *testing.T) {
	acc := newFakeAccount()
	acc.chats = []project.ChatRow{
		{ID: 42, Title: "Alice", IsUser: true, Unread: 3, Mentions: 1},
		{ID: 1007, Title: "Old news", Archived: true, Unread: 9},
		{ID: 7, Title: "Work", Muted: true, Reactions: 2},
		{ID: 300, Title: "Gophers", IsForum: true, Unread: 1, UnreadMark: true},
		{ID: 5, Title: "Bob"},
	}
	var out bytes.Buffer

	if err := listChats(shortCtx(t), acc, &out); err != nil {
		t.Fatal(err)
	}

	want := "" +
		"42   Alice    3 unread, 1 mention\n" +
		"7    Work     2 reactions, muted\n" +
		"300  Gophers  forum, 1 topic unread, marked unread\n" +
		"5    Bob\n"
	if out.String() != want {
		t.Errorf("printed\n%s\nwant\n%s", out.String(), want)
	}
}
