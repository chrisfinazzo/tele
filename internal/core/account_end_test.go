package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/domain"
	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// What ends an account is told to the person on the way back to the login, so
// it is named from a closed list rather than read off Telegram's errors (#297).
func TestEndReasonOf_ALogOutEndsTheAccount(t *testing.T) {
	for logOut, want := range map[telerr.LogOut]EndReason{
		telerr.LogOutElsewhere:  EndLoggedOutElsewhere,
		telerr.LogOutDeleted:    EndAccountDeleted,
		telerr.LogOutKeyDropped: EndKeyInvalidated,
	} {
		got, ok := EndReasonOf(&telerr.Error{Kind: telerr.Unauthorized, LogOut: logOut})
		assert.True(t, ok, logOut)
		assert.Equal(t, want, got, logOut)
	}
}

// A connection that ended for any other reason leaves the account as it is: a
// dropped network is not a log out, and nothing of the account is removed.
func TestEndReasonOf_AnythingElseEndsNothing(t *testing.T) {
	for _, err := range []error{
		nil,
		&telerr.Error{Kind: telerr.Network, Transient: true},
		&telerr.Error{Kind: telerr.AccountBanned},
		&telerr.Error{Kind: telerr.Unauthorized},
		assert.AnError,
	} {
		_, ok := EndReasonOf(err)
		assert.False(t, ok, "%v", err)
	}
}

// A sent message waits in the queue only until its update arrives, and is not
// lost with the account: it is already Telegram's.
func TestUnsentCount_CountsWhatHasNotGoneOut(t *testing.T) {
	o, _, _ := newTestOwner(t)
	assert.Zero(t, o.UnsentCount(), "no send queue, nothing unsent")

	q := newOutboxStore(t)
	o.SetOutbox(q)
	for _, ref := range []string{"queued", "failed", "sent"} {
		_, _, err := q.Add(domain.OutboxEntry{Ref: ref, ChatID: 1, State: domain.OutboxQueued})
		require.NoError(t, err)
	}
	failed, _ := q.Get("failed")
	failed.State = domain.OutboxFailed
	require.NoError(t, q.Update(failed))
	sent, _ := q.Get("sent")
	sent.State, sent.SentMsgIDs = domain.OutboxSending, []int{7}
	require.NoError(t, q.Update(sent))

	assert.Equal(t, 2, o.UnsentCount())
}
