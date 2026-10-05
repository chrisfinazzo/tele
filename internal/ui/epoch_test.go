package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/ui/components"
)

type probeMsg struct{ n int }

// A message a command produces is marked with the epoch of the model that
// asked for it, so a model built for the next account can tell it apart (#297).
func TestStamp_MarksWhatACommandProduces(t *testing.T) {
	cmd := stamp(3, func() tea.Msg { return probeMsg{1} })

	assert.Equal(t, epochMsg{epoch: 3, msg: probeMsg{1}}, cmd())
}

// bubbletea's own messages are handled by the program before any model sees
// them, and carry nothing of an account: wrapped, a quit would never quit.
func TestStamp_LeavesTheProgramsOwnMessagesAlone(t *testing.T) {
	assert.Equal(t, tea.QuitMsg{}, stamp(3, tea.Quit)())
}

// A batch or a sequence is a list of commands the program runs itself; each of
// them is marked instead, and a sequence stays a sequence.
func TestStamp_MarksEachCommandOfABatchAndASequence(t *testing.T) {
	one := func() tea.Msg { return probeMsg{1} }
	two := func() tea.Msg { return probeMsg{2} }

	batch, ok := stamp(3, tea.Batch(one, two))().(tea.BatchMsg)
	require.True(t, ok, "a batch stays a batch")
	require.Len(t, batch, 2)
	assert.Equal(t, epochMsg{epoch: 3, msg: probeMsg{1}}, batch[0]())

	seqCmd := tea.Sequence(one, two)
	seq := stamp(3, seqCmd)()
	assert.IsType(t, seqCmd(), seq, "a sequence stays a sequence")
}

func TestStamp_NothingToStamp(t *testing.T) {
	assert.Nil(t, stamp(3, nil))
}

// What a previous account's model asked for is dropped on arrival: a history
// page or a profile from the account that just ended must not land in the
// next one's screen (#297).
func TestShell_DropsWhatAnotherEpochAskedFor(t *testing.T) {
	s := NewShell(2, mainScreenModel())

	next, cmd := s.Update(epochMsg{epoch: 1, msg: StatusErrMsg{Text: "from the account that ended"}})

	assert.Nil(t, cmd)
	assert.True(t, next.(Shell).root.toasts.Empty())
}

func TestShell_TakesWhatItsOwnEpochAskedFor(t *testing.T) {
	s := NewShell(2, mainScreenModel())

	next, _ := s.Update(epochMsg{epoch: 2, msg: StatusErrMsg{Text: "ours", Sev: components.SeverityError}})

	assert.False(t, next.(Shell).root.toasts.Empty())
}

// Input from the terminal belongs to no account and always arrives.
func TestShell_TakesWhatNoEpochMarked(t *testing.T) {
	s := NewShell(2, mainScreenModel())

	next, _ := s.Update(StatusErrMsg{Text: "unmarked", Sev: components.SeverityError})

	assert.False(t, next.(Shell).root.toasts.Empty())
}
