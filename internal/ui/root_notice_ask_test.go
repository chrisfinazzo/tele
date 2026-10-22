package ui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/notices"
)

// askingNotice is a notice that asks, and counts how often it was told yes.
func askingNotice(yes *int) notices.Notice {
	return notices.Notice{
		ID: "asks", Title: "Install it?", Body: "body",
		Yes: func(context.Context) (string, error) {
			*yes++
			return "done", nil
		},
	}
}

func pressKey(t *testing.T, m RootModel, k tea.KeyPressMsg) (RootModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(k)
	return next.(RootModel), cmd
}

// runCmd executes cmd and every command it batches, as the program would.
func runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmd(c)
		}
	}
}

func TestNoticeAsk_YesRunsTheAnswerAndIsSeen(t *testing.T) {
	yes := 0
	seen := notices.NewMemorySeen()
	m := NewRootModel(false).WithNotices([]notices.Notice{askingNotice(&yes)}, seen)
	m.width, m.height = 100, 30

	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	runCmd(cmd)

	assert.Equal(t, 1, yes, "y answers yes")
	assert.True(t, seen.IsSeen("asks"))
	assert.NotContains(t, xansi.Strip(m.View().Content), "Install it?")
}

func TestNoticeAsk_NoAndEscAnswerNo(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: 'n', Text: "n"}, {Code: tea.KeyEsc}} {
		yes := 0
		seen := notices.NewMemorySeen()
		m := NewRootModel(false).WithNotices([]notices.Notice{askingNotice(&yes)}, seen)
		m.width, m.height = 100, 30

		m, cmd := pressKey(t, m, k)
		runCmd(cmd)

		assert.Zero(t, yes, "%s answers no", k)
		assert.True(t, seen.IsSeen("asks"), "a no is an answer too")
		assert.NotContains(t, xansi.Strip(m.View().Content), "Install it?")
	}
}

// A key typed out of habit, Enter after the login above all, must not answer
// a question that is asked only once.
func TestNoticeAsk_OtherKeysDoNotAnswer(t *testing.T) {
	yes := 0
	seen := notices.NewMemorySeen()
	m := NewRootModel(false).WithNotices([]notices.Notice{askingNotice(&yes)}, seen)
	m.width, m.height = 100, 30

	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: 'x', Text: "x"}, {Code: tea.KeySpace, Text: " "}} {
		var cmd tea.Cmd
		m, cmd = pressKey(t, m, k)
		runCmd(cmd)
	}

	assert.Zero(t, yes)
	assert.False(t, seen.IsSeen("asks"))
	out := xansi.Strip(m.View().Content)
	require.Contains(t, out, "Install it?", "the question stays up")
	assert.Contains(t, out, "y / n", "and says how to answer it")
}
