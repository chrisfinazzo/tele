package tg

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	gotdtg "github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// throttleClock is the throttle's time, moved only by the waits it is asked
// for, which it records.
type throttleClock struct {
	now   time.Time
	waits []time.Duration
}

func (c *throttleClock) Now() time.Time { return c.now }

func (c *throttleClock) Sleep(_ context.Context, d time.Duration) error {
	c.waits = append(c.waits, d)
	c.now = c.now.Add(d)
	return nil
}

// telegramFake is Telegram behind the throttle: it records which methods
// reached it and answers each with what answer says.
type telegramFake struct {
	reached []string
	answer  func(op string) error
}

func (f *telegramFake) Invoke(_ context.Context, input bin.Encoder, _ bin.Decoder) error {
	op := opName(input)
	f.reached = append(f.reached, op)
	if f.answer == nil {
		return nil
	}
	return f.answer(op)
}

// floodOnce answers the first call of op with a flood wait of d.
func floodOnce(op string, d time.Duration) func(string) error {
	flooded := false
	return func(got string) error {
		if got == op && !flooded {
			flooded = true
			return &telerr.Error{Kind: telerr.RateLimited, Op: op, RetryAfter: d}
		}
		return nil
	}
}

func throttled(t *testing.T, tg *telegramFake) (*throttleClock, gotdtg.Invoker) {
	t.Helper()
	clock := &throttleClock{now: time.Unix(1_700_000_000, 0)}
	th := newThrottle(clock.Now, clock.Sleep)
	return clock, th.middleware().Handle(tg)
}

func call(inv gotdtg.Invoker, req bin.Encoder) error {
	return inv.Invoke(context.Background(), req, nil)
}

const (
	getHistory = "messages.getHistory"
	sendText   = "messages.sendMessage"
)

// A flood wait holds every later call of the method that earned it until the
// wait is over, so none of them reaches Telegram to earn a longer one (#212).
func TestThrottle_AFloodWaitHoldsTheMethodUntilItIsOver(t *testing.T) {
	tg := &telegramFake{answer: floodOnce(getHistory, 30*time.Second)}
	clock, inv := throttled(t, tg)

	require.Error(t, call(inv, &gotdtg.MessagesGetHistoryRequest{}))
	clock.now = clock.now.Add(10 * time.Second)
	require.NoError(t, call(inv, &gotdtg.MessagesGetHistoryRequest{}))

	assert.Equal(t, []time.Duration{20 * time.Second}, clock.waits, "the second call sat out the rest of the wait")
	assert.Equal(t, []string{getHistory, getHistory}, tg.reached, "and only then went to Telegram")
}

// Telegram scopes a flood wait to the method, so the other methods go on.
func TestThrottle_OtherMethodsAreNotHeld(t *testing.T) {
	tg := &telegramFake{answer: floodOnce(getHistory, 30*time.Second)}
	clock, inv := throttled(t, tg)

	require.Error(t, call(inv, &gotdtg.MessagesGetHistoryRequest{}))
	require.NoError(t, call(inv, &gotdtg.MessagesSendMessageRequest{}))

	assert.Empty(t, clock.waits)
	assert.Equal(t, []string{getHistory, sendText}, tg.reached)
}

// A wait longer than a call would sit out is refused at once, with what is
// left of it, and Telegram is not asked again meanwhile.
func TestThrottle_AWaitTooLongToSitOutIsRefusedWithoutAsking(t *testing.T) {
	tg := &telegramFake{answer: floodOnce(getHistory, 10*time.Minute)}
	clock, inv := throttled(t, tg)

	require.Error(t, call(inv, &gotdtg.MessagesGetHistoryRequest{}))
	clock.now = clock.now.Add(time.Minute)
	err := call(inv, &gotdtg.MessagesGetHistoryRequest{})

	e, ok := telerr.As(err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, telerr.RateLimited, e.Kind)
	assert.Equal(t, 9*time.Minute, e.RetryAfter)
	assert.Equal(t, getHistory, e.Op)
	assert.Empty(t, clock.waits)
	assert.Equal(t, []string{getHistory}, tg.reached)
}

func TestThrottle_AWaitThatIsOverHoldsNothing(t *testing.T) {
	tg := &telegramFake{answer: floodOnce(getHistory, 30*time.Second)}
	clock, inv := throttled(t, tg)

	require.Error(t, call(inv, &gotdtg.MessagesGetHistoryRequest{}))
	clock.now = clock.now.Add(31 * time.Second)
	require.NoError(t, call(inv, &gotdtg.MessagesGetHistoryRequest{}))

	assert.Empty(t, clock.waits)
	assert.Len(t, tg.reached, 2)
}
