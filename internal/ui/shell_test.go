package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/sorokin-vladimir/tele/internal/core"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
	"github.com/sorokin-vladimir/tele/internal/ui/components"
	"github.com/sorokin-vladimir/tele/internal/ui/screens"
)

func nextAccountRoot() RootModel {
	m := NewRootModel(false)
	m.SetLoginModel(screens.NewLoginModel(internaltg.NewAuthFlow()))
	return m
}

// An account that ended is replaced on screen by the next one's login, drawn at
// the size the terminal already has, and from then on only the next account's
// messages arrive (#297).
func TestShell_AnEndedAccountGivesWayToTheNextOne(t *testing.T) {
	s := NewShell(1, mainScreenModel())

	next, cmd := s.Update(AccountEndedMsg{
		Epoch: 2, Root: nextAccountRoot(),
		Ended: core.AccountEnded{Reason: core.EndLoggedOutElsewhere},
	})
	s = next.(Shell)

	assert.NotNil(t, cmd, "the next model starts")
	assert.Equal(t, uint64(2), s.epoch)
	assert.Equal(t, ScreenLogin, s.root.screen)
	assert.Equal(t, 100, s.root.width, "the terminal did not change size")

	late, _ := s.Update(epochMsg{epoch: 1, msg: StatusErrMsg{Text: "late", Sev: components.SeverityError}})
	assert.True(t, late.(Shell).root.toasts.Empty(), "the ended account is not heard from again")
}

// The person is told why they are back at the login, under the number field,
// and how many unsent messages went with the account.
func TestShell_TheNextLoginSaysWhyTheLastAccountEnded(t *testing.T) {
	s := NewShell(1, mainScreenModel())
	next, _ := s.Update(AccountEndedMsg{
		Epoch: 2, Root: nextAccountRoot(),
		Ended: core.AccountEnded{Reason: core.EndLoggedOutElsewhere, Discarded: 3},
	})

	next, _ = next.Update(epochMsg{epoch: 2, msg: screens.AuthRequestMsg{Step: internaltg.AuthStepPhone}})

	view := xansi.Strip(next.View().Content)
	assert.Contains(t, view, "This session was logged out.")
	assert.Contains(t, view, "3 unsent messages were discarded with it.")
}

func TestLoginNotice_NamesEachEnd(t *testing.T) {
	for reason, want := range map[core.EndReason]string{
		core.EndLoggedOutHere:      "Logged out.",
		core.EndLoggedOutElsewhere: "This session was logged out.",
		core.EndAccountDeleted:     "This Telegram account was deleted.",
		core.EndKeyInvalidated:     "Telegram invalidated this session's key",
		core.EndBanned:             "Log in with another number.",
	} {
		assert.Contains(t, loginNotice(core.AccountEnded{Reason: reason}), want, reason)
	}
	assert.Contains(t, loginNotice(core.AccountEnded{Reason: core.EndLoggedOutHere, Discarded: 1}),
		"1 unsent message was discarded with it.")
	assert.NotContains(t, loginNotice(core.AccountEnded{Reason: core.EndLoggedOutHere}), "unsent")
}

// The tick that kept the old model's screen alive is not the new one's.
func TestShell_ResizeAfterTheSwitchReachesTheNextModel(t *testing.T) {
	s := NewShell(1, mainScreenModel())
	next, _ := s.Update(AccountEndedMsg{Epoch: 2, Root: nextAccountRoot()})

	next, _ = next.Update(tea.WindowSizeMsg{Width: 90, Height: 30})

	assert.Equal(t, 90, next.(Shell).root.width)
}
