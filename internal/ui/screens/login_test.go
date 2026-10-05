package screens_test

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	internaltg "github.com/sorokin-vladimir/tele/internal/tg"
	"github.com/sorokin-vladimir/tele/internal/ui/screens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogin_AuthRequest_UpdatesPrompt(t *testing.T) {
	af := internaltg.NewAuthFlow()
	m := screens.NewLoginModel(af)
	newM, _ := m.Update(screens.AuthRequestMsg{Step: internaltg.AuthStepPhone})
	lm := newM.(screens.LoginModel)
	assert.Equal(t, internaltg.AuthStepPhone, lm.CurrentStep())
	assert.Contains(t, lm.View().Content, "phone")
}

func TestLogin_Connected_EmitsTransition(t *testing.T) {
	af := internaltg.NewAuthFlow()
	m := screens.NewLoginModel(af)
	_, cmd := m.Update(screens.ConnectedMsg{})
	require.NotNil(t, cmd)
	msg := cmd()
	assert.IsType(t, screens.TransitionToMainMsg{}, msg)
}

func TestLogin_InitialView_ShowsConnecting(t *testing.T) {
	af := internaltg.NewAuthFlow()
	m := screens.NewLoginModel(af)
	assert.Contains(t, m.View().Content, "onnect") // "Connecting..." or "Connect"
}

// The slow mark belongs to the wait for Telegram's first answer. Once a prompt
// is up the wait is over, and a tick that arrives late must not bring it back.
func TestLogin_SlowConnectMarksOnlyTheWait(t *testing.T) {
	m := screens.NewLoginModel(internaltg.NewAuthFlow())
	assert.False(t, m.Slow())

	slow, _ := m.Update(screens.SlowConnectMsg{})
	assert.True(t, slow.(screens.LoginModel).Slow())

	prompted, _ := m.Update(screens.AuthRequestMsg{Step: internaltg.AuthStepPhone})
	late, _ := prompted.(screens.LoginModel).Update(screens.SlowConnectMsg{})
	assert.False(t, late.(screens.LoginModel).Slow())
}

// A prompt arriving after the mark ends the wait too.
func TestLogin_PromptEndsTheSlowWait(t *testing.T) {
	m := screens.NewLoginModel(internaltg.NewAuthFlow())
	slow, _ := m.Update(screens.SlowConnectMsg{})

	prompted, _ := slow.(screens.LoginModel).Update(screens.AuthRequestMsg{Step: internaltg.AuthStepPhone})

	assert.False(t, prompted.(screens.LoginModel).Slow())
}

// A step asked again says why under the field, and the reason goes away with
// the next step that has none (#285).
func TestLogin_ReasonShowsUnderTheField(t *testing.T) {
	m := screens.NewLoginModel(internaltg.NewAuthFlow())

	again, _ := m.Update(screens.AuthRequestMsg{Step: internaltg.AuthStepCode, Err: "That code is not right. Try again."})
	assert.Contains(t, again.(screens.LoginModel).View().Content, "That code is not right. Try again.")

	onward, _ := again.(screens.LoginModel).Update(screens.AuthRequestMsg{Step: internaltg.AuthStepPassword})
	assert.NotContains(t, onward.(screens.LoginModel).View().Content, "not right")
}

// submit presses enter on the login screen and returns what reached the login.
func submit(t *testing.T, af *internaltg.AuthFlow, m screens.LoginModel) string {
	t.Helper()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case resp := <-af.Responses:
		return resp.Value
	case <-time.After(time.Second):
		require.FailNow(t, "nothing was submitted")
		return ""
	}
}

func typed(m screens.LoginModel, s string) screens.LoginModel {
	next, _ := m.Update(tea.PasteMsg{Content: s})
	return next.(screens.LoginModel)
}

// Copying a number or a code tends to bring a space or a line break with it,
// and Telegram would refuse the value for it (#284).
func TestLogin_PhoneAndCodeAreTrimmed(t *testing.T) {
	for _, step := range []internaltg.AuthStep{internaltg.AuthStepPhone, internaltg.AuthStepCode} {
		af := internaltg.NewAuthFlow()
		m, _ := screens.NewLoginModel(af).Update(screens.AuthRequestMsg{Step: step})

		assert.Equal(t, "12345", submit(t, af, typed(m.(screens.LoginModel), " 12345 ")))
	}
}

// A password is sent exactly as typed: a space may be part of it, and Telegram
// trims nothing.
func TestLogin_PasswordIsNotTrimmed(t *testing.T) {
	af := internaltg.NewAuthFlow()
	m, _ := screens.NewLoginModel(af).Update(screens.AuthRequestMsg{Step: internaltg.AuthStepPassword})

	assert.Equal(t, " secret ", submit(t, af, typed(m.(screens.LoginModel), " secret ")))
}

// A notice says why the person is at the login. It is shown under the number
// the first time the number is asked for, and gives way to a reason the number
// is asked again (#297).
func TestLogin_NoticeShowsUnderTheFirstNumberStep(t *testing.T) {
	m := screens.NewLoginModel(internaltg.NewAuthFlow())
	m.SetNotice("This session was logged out.")

	first, _ := m.Update(screens.AuthRequestMsg{Step: internaltg.AuthStepPhone})
	assert.Contains(t, first.View().Content, "This session was logged out.")

	again, _ := first.Update(screens.AuthRequestMsg{Step: internaltg.AuthStepPhone, Err: "That number is not valid."})
	assert.NotContains(t, again.View().Content, "This session was logged out.")
	assert.Contains(t, again.View().Content, "That number is not valid.")
}

// The wait for the next login step ends with the account: a login nobody
// finished must not hold the account's end up forever (#297).
func TestWaitForAuthRequest_EndsWithTheAccount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan tea.Msg, 1)
	go func() { done <- screens.WaitForAuthRequest(ctx, internaltg.NewAuthFlow(), make(chan struct{}))() }()

	select {
	case msg := <-done:
		assert.Nil(t, msg)
	case <-time.After(time.Second):
		require.Fail(t, "the wait outlived the account")
	}
}

// ensure tea import is used (Blink cmd returns tea.Cmd)
var _ tea.Cmd = screens.NewLoginModel(nil).Init()
