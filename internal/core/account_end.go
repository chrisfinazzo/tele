package core

import "github.com/sorokin-vladimir/tele/internal/telerr"

// EndReason is why an account ended while tele ran. The set is closed: it is
// what a client tells the person on the way back to the login (#297).
type EndReason string

const (
	// EndLoggedOutHere means the person logged out from this client.
	EndLoggedOutHere EndReason = "logged_out_here"
	// EndLoggedOutElsewhere means the session was ended from another device or
	// by Telegram.
	EndLoggedOutElsewhere EndReason = "logged_out_elsewhere"
	// EndAccountDeleted means the Telegram account behind the session was
	// deleted.
	EndAccountDeleted EndReason = "account_deleted"
	// EndKeyInvalidated means Telegram invalidated the session's key because it
	// was used from two connections at once.
	EndKeyInvalidated EndReason = "key_invalidated"
	// EndBanned means the person left a banned account to log in with another
	// number.
	EndBanned EndReason = "banned"
)

// AccountEnded tells a client that the account it was showing has ended and
// its files are gone, and why. Discarded counts the messages that were still
// waiting to be sent and went with it.
type AccountEnded struct {
	Reason    EndReason
	Discarded int
}

// EndReasonOf reports whether the connection ended with a log out, and which.
// Only a log out ends an account: anything else the connection ends with
// leaves the account and its files as they are.
func EndReasonOf(err error) (EndReason, bool) {
	e, ok := telerr.As(err)
	if !ok || e.Kind != telerr.Unauthorized {
		return "", false
	}
	switch e.LogOut {
	case telerr.LogOutElsewhere:
		return EndLoggedOutElsewhere, true
	case telerr.LogOutDeleted:
		return EndAccountDeleted, true
	case telerr.LogOutKeyDropped:
		return EndKeyInvalidated, true
	default:
		return "", false
	}
}

// UnsentCount is how many messages in the send queue have not gone out yet:
// what an account that ends now takes with it.
func (o *Owner) UnsentCount() int {
	if o.outbox == nil {
		return 0
	}
	n := 0
	for _, e := range o.outbox.All() {
		if len(e.SentMsgIDs) == 0 {
			n++
		}
	}
	return n
}
