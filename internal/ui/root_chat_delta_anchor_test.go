package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/store"
)

// runCmdTree runs a command and everything it batches, so a test can assert on
// what the commands did rather than on their shape.
func runCmdTree(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmdTree(c)
		}
	}
}

// A chat that opens anchored on its first unread message still has to fetch the
// media in its window. The anchor only says the window is already positioned, so
// the scroll is skipped — everything else the open path does still applies.
// Until this was fixed, opening a chat with unread messages left every photo and
// video poster in it blank until the user scrolled far enough to trigger a
// window delta.
func TestChatReset_AnchoredOnFirstUnread_StillFetchesMedia(t *testing.T) {
	o, m := anchorTestModel()

	_, cmd := m.handleChatDelta(&project.HistoryDelta{
		Kind: project.HistoryReset,
		Contents: project.HistoryContents{
			ChatID:   1,
			Messages: mediaWindow(),
			// The window was pinned to the first unread (#202): the anchor is set
			// and sits at or below the read pointer.
			AnchorMsgID:    7,
			ReadInboxMaxID: 7,
		},
	})

	runCmdTree(cmd)

	if len(o.fetched) == 0 {
		t.Fatal("an anchored chat open must still fetch the media in its window")
	}
	if o.fetched[0] != (mediaKey{1, 7, domain.PhotoThumb}) {
		t.Fatalf("fetched %v, want the window's photo thumbnail", o.fetched)
	}
}

// The unanchored open path already fetched its media; it must keep doing so.
func TestChatReset_Unanchored_FetchesMedia(t *testing.T) {
	o, m := anchorTestModel()

	_, cmd := m.handleChatDelta(&project.HistoryDelta{
		Kind:     project.HistoryReset,
		Contents: project.HistoryContents{ChatID: 1, Messages: mediaWindow()},
	})

	runCmdTree(cmd)

	if len(o.fetched) == 0 {
		t.Fatal("an unanchored chat open must fetch the media in its window")
	}
}

// A refreshed file reference reaches the window as an in-place update of a
// message already on screen: nothing the user can see changed, but the media
// behind it is addressable again. The window has to ask for it, because the
// fetch that failed is what triggered the refresh and nothing else will.
func TestChatUpdate_FetchesTheMediaOfTheUpdatedMessage(t *testing.T) {
	o, m := anchorTestModel()
	m.chatMsgs = mediaWindow()

	_, cmd := m.handleChatDelta(&project.HistoryDelta{
		Kind:    project.HistoryUpdate,
		Message: mediaWindow()[0],
	})

	runCmdTree(cmd)

	if len(o.fetched) == 0 {
		t.Fatal("an updated message must have its media fetched")
	}
	if o.fetched[0] != (mediaKey{1, 7, domain.PhotoThumb}) {
		t.Fatalf("fetched %v, want the updated message's photo thumbnail", o.fetched)
	}
}

func anchorTestModel() (*ownerStub, RootModel) {
	st := store.NewMemory()
	o := newOwnerStub(st)
	m := newRootInternal(st, 50).WithOwner(o).WithScreen(ScreenMain)
	m.currentChatID = 1
	m.focus = FocusChat
	return o, m
}

func mediaWindow() []domain.Message {
	return []domain.Message{{
		ID: 7, ChatID: 1, Date: time.Unix(1, 0),
		Media: &domain.MediaRef{Kind: domain.MediaPhoto},
		Photo: &domain.PhotoRef{ID: 42, ThumbSize: "m"},
	}}
}
