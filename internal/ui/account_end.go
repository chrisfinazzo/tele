package ui

import (
	"strconv"

	"github.com/sorokin-vladimir/tele/internal/core"
)

// AccountEndedMsg is the host telling the client that the account on screen
// has ended and its files are gone. It brings the model for the next account,
// built by the host, and the epoch that model draws (#297). It is never
// stamped: it is what changes the epoch.
type AccountEndedMsg struct {
	Epoch uint64
	Root  RootModel
	Ended core.AccountEnded
}

// loginNotice is what the next login says about the account that ended: why
// the person is back here, and what was lost that they cannot get back from
// Telegram. The login box does not wrap, so lines are kept short.
func loginNotice(e core.AccountEnded) string {
	var text string
	switch e.Reason {
	case core.EndLoggedOutHere:
		text = "Logged out."
	case core.EndLoggedOutElsewhere:
		text = "This session was logged out.\nLog in again."
	case core.EndAccountDeleted:
		text = "This Telegram account was deleted.\nLog in to start again."
	case core.EndKeyInvalidated:
		text = "Telegram invalidated this session's key:\nit was used from two connections at once.\nLog in again."
	case core.EndBanned:
		// The ban may have been the number's, before any account was
		// logged in, so nothing here claims one was left.
		text = "Log in with another number."
	}
	switch {
	case e.Discarded == 1:
		text += "\n1 unsent message was discarded with it."
	case e.Discarded > 1:
		text += "\n" + strconv.Itoa(e.Discarded) + " unsent messages were discarded with it."
	}
	return text
}
