package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An owner lasts one account, and stopping it is what lets its host remove the
// account's files: nothing the owner started may still be touching them once
// Stop returns (#297).
func TestOwner_StopWaitsForItsBackgroundWork(t *testing.T) {
	o, _, _ := newTestOwner(t)
	started := make(chan struct{})
	var finished atomic.Bool
	o.spawn(func() {
		close(started)
		<-o.ctx.Done()
		time.Sleep(20 * time.Millisecond)
		finished.Store(true)
	})
	<-started

	o.Stop()

	assert.True(t, finished.Load(), "Stop returned while the work was still running")
}

// Work asked for after the owner stopped is not started: a client's command can
// arrive while the account is ending, and must not reach a closed database.
func TestOwner_StartsNoWorkOnceStopped(t *testing.T) {
	o, _, _ := newTestOwner(t)
	o.Stop()

	ran := make(chan struct{}, 1)
	o.spawn(func() { ran <- struct{}{} })

	select {
	case <-ran:
		require.Fail(t, "work started after Stop")
	case <-time.After(50 * time.Millisecond):
	}
}

// The backstop for a sent message with no update sleeps out a grace period; a
// stopping owner does not wait for it.
func TestOwner_StopDoesNotWaitOutTheSentGracePeriod(t *testing.T) {
	o, _, _ := newTestOwner(t)
	o.spawn(func() { o.dropIfUndelivered("ref") })

	done := make(chan struct{})
	go func() { o.Stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		require.Fail(t, "Stop waited out the grace period")
	}
}
