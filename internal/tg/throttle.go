package tg

import (
	"context"
	"sync"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	gotdtg "github.com/gotd/td/tg"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// throttle is the owner's one outbound budget, shared by every call to
// Telegram whoever makes it (#212). A call that is answered with a flood wait
// holds every later call of the same method until the wait is over: Telegram
// scopes a flood wait to the method that earned it, and a caller that asked
// again meanwhile would only earn a longer one.
type throttle struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error

	mu sync.Mutex
	// held is when each method held by a flood wait may be called again.
	held map[string]time.Time
}

func newThrottle(now func() time.Time, sleep func(context.Context, time.Duration) error) *throttle {
	return &throttle{now: now, sleep: sleep, held: map[string]time.Time{}}
}

// sleepCtx waits for d or until ctx ends, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// middleware puts every call through the throttle. It sits outside the error
// mapping, so the flood waits it reads are already domain errors.
func (th *throttle) middleware() telegram.Middleware {
	return telegram.MiddlewareFunc(func(next gotdtg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			op := opName(input)
			if err := th.wait(ctx, op); err != nil {
				return err
			}
			err := next.Invoke(ctx, input, output)
			if e, ok := telerr.As(err); ok && e.Kind == telerr.RateLimited {
				th.hold(op, e.RetryAfter)
			}
			return err
		}
	})
}

// wait sits out what is left of op's flood wait, when a call would sit it out
// anyway. A longer one is refused at once with what is left of it, the same
// answer Telegram would give, without asking Telegram again.
func (th *throttle) wait(ctx context.Context, op string) error {
	th.mu.Lock()
	left := th.held[op].Sub(th.now())
	th.mu.Unlock()
	if left <= 0 {
		return nil
	}
	if left > rateLimitBudget {
		return &telerr.Error{Kind: telerr.RateLimited, Op: op, RetryAfter: left}
	}
	return th.sleep(ctx, left)
}

// hold keeps op from being called for d from now, unless it is held longer
// already.
func (th *throttle) hold(op string, d time.Duration) {
	th.mu.Lock()
	defer th.mu.Unlock()
	if until := th.now().Add(d); until.After(th.held[op]) {
		th.held[op] = until
	}
}
