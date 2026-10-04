package core

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sorokin-vladimir/tele/internal/domain"
)

func TestFocusRegistry_AttachGivesDistinctIDs(t *testing.T) {
	r := newFocusRegistry()
	a, b := r.attach(), r.attach()
	assert.NotEqual(t, a, b, "each attachment needs its own identity")
}

func TestFocusRegistry_AttachedClientStartsWithNoChat(t *testing.T) {
	r := newFocusRegistry()
	r.attach()
	assert.False(t, r.focused(7), "a client that has not opened a chat focuses nothing")
}

func TestFocusRegistry_SetIsVisibleToFocused(t *testing.T) {
	r := newFocusRegistry()
	id := r.attach()
	r.set(id, domain.HistoryKey{ChatID: 7})
	assert.True(t, r.focused(7))
	assert.False(t, r.focused(8))
}

// A client shows one history. In a forum that is one topic: the chat is on
// screen, the other topics of it are not (#275).
func TestFocusRegistry_ATopicOnScreenIsThatTopicAlone(t *testing.T) {
	r := newFocusRegistry()
	id := r.attach()
	r.set(id, domain.HistoryKey{ChatID: 50, TopicID: 12})

	assert.True(t, r.showing(domain.HistoryKey{ChatID: 50, TopicID: 12}))
	assert.False(t, r.showing(domain.HistoryKey{ChatID: 50, TopicID: 30}))
	assert.True(t, r.focused(50), "the forum is on screen through its topic")
}

func TestFocusRegistry_SetZeroClearsFocus(t *testing.T) {
	r := newFocusRegistry()
	id := r.attach()
	r.set(id, domain.HistoryKey{ChatID: 7})
	r.set(id, domain.HistoryKey{})
	assert.False(t, r.focused(7), "closing a chat must release its focus")
}

func TestFocusRegistry_DetachClearsFocus(t *testing.T) {
	r := newFocusRegistry()
	id := r.attach()
	r.set(id, domain.HistoryKey{ChatID: 7})
	r.detach(id)
	assert.False(t, r.focused(7), "a client that went away must not silence its last chat forever")
}

func TestFocusRegistry_NoClientsFocusNothing(t *testing.T) {
	r := newFocusRegistry()
	assert.False(t, r.focused(7), "with nobody attached there is nothing to suppress")
}

func TestFocusRegistry_OneOfTwoClientsIsEnough(t *testing.T) {
	r := newFocusRegistry()
	a, b := r.attach(), r.attach()
	r.set(a, domain.HistoryKey{ChatID: 7})
	r.set(b, domain.HistoryKey{ChatID: 9})
	assert.True(t, r.focused(7))
	assert.True(t, r.focused(9))

	r.detach(a)
	assert.False(t, r.focused(7), "detaching the only client watching 7 releases it")
	assert.True(t, r.focused(9), "the other client is unaffected")
}

func TestFocusRegistry_SetOnDetachedClientIsIgnored(t *testing.T) {
	r := newFocusRegistry()
	id := r.attach()
	r.detach(id)
	r.set(id, domain.HistoryKey{ChatID: 7})
	assert.False(t, r.focused(7), "a detached client must not resurrect its focus")
}

func TestFocusRegistry_ZeroChatIsNeverFocused(t *testing.T) {
	r := newFocusRegistry()
	r.attach()
	assert.False(t, r.focused(0), "0 is how a client says it has no chat open, not a chat")
}

func TestFocusRegistry_ConcurrentSetAndFocused(t *testing.T) {
	// The update loop reads focus while a client writes it. Meaningful under -race.
	r := newFocusRegistry()
	id := r.attach()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			r.set(id, domain.HistoryKey{ChatID: int64(i % 3)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = r.focused(1)
		}
	}()
	wg.Wait()
}

func TestAttachment_ReportsAndReleasesFocus(t *testing.T) {
	o := &Owner{focus: newFocusRegistry()}
	a := o.Attach()

	a.SetFocus(domain.HistoryKey{ChatID: 7})
	assert.True(t, o.focus.focused(7))

	a.SetFocus(domain.HistoryKey{})
	assert.False(t, o.focus.focused(7), "a client that closed its chat focuses nothing")

	a.SetFocus(domain.HistoryKey{ChatID: 7})
	a.Detach()
	assert.False(t, o.focus.focused(7), "a client that detached takes its focus with it")
}

func TestAttachment_TwoClientsAreIndependent(t *testing.T) {
	o := &Owner{focus: newFocusRegistry()}
	a, b := o.Attach(), o.Attach()
	a.SetFocus(domain.HistoryKey{ChatID: 7})
	b.SetFocus(domain.HistoryKey{ChatID: 9})

	a.Detach()
	assert.False(t, o.focus.focused(7))
	assert.True(t, o.focus.focused(9), "one client detaching must not disturb another")
}
