package screens_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/ui/keys"
	"github.com/sorokin-vladimir/tele/internal/ui/screens"
)

func topicList(rows ...project.TopicRow) *screens.TopicListModel {
	m := screens.NewTopicListModel(50, "Dev")
	m.SetSize(40, 20)
	m.SetWindow(0, len(rows), rows)
	return m
}

func viewLines(m *screens.TopicListModel) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// A topic row reads like a chat row with a topic's marker in front: open
// topics are marked #, closed ones -, and a topic nothing was learned about yet
// is named by its id.
func TestTopicList_RowsAreMarkedAndNamed(t *testing.T) {
	m := topicList(
		project.TopicRow{ID: 12, Title: "Releases", Unread: 3, Mentions: 1, Muted: true},
		project.TopicRow{ID: 30, Title: "Design", Closed: true},
		project.TopicRow{ID: 31},
	)

	lines := viewLines(m)

	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "# Releases")
	assert.Contains(t, lines[0], "× @ [3]")
	assert.Contains(t, lines[1], "- Design")
	assert.Contains(t, lines[2], "# Topic 31")
}

func TestTopicList_ConfirmOpensTheTopic(t *testing.T) {
	m := topicList(project.TopicRow{ID: 12, Title: "Releases"}, project.TopicRow{ID: 30, Title: "Design"})

	m.Update(keys.ActionMsg{Action: keys.ActionDown})
	_, cmd := m.Update(keys.ActionMsg{Action: keys.ActionConfirm})

	require.NotNil(t, cmd)
	assert.Equal(t, screens.OpenTopicMsg{ChatID: 50, TopicID: 30, Title: "Design"}, cmd())
}

func TestTopicList_CancelLeavesTheForum(t *testing.T) {
	m := topicList(project.TopicRow{ID: 12, Title: "Releases"})

	_, cmd := m.Update(keys.ActionMsg{Action: keys.ActionCancel})

	require.NotNil(t, cmd)
	assert.Equal(t, screens.LeaveForumMsg{ChatID: 50}, cmd())
}

func TestTopicList_ATopicNotYetDescribedIsTitledByItsID(t *testing.T) {
	assert.Equal(t, "Topic 31", screens.TopicTitle(project.TopicRow{ID: 31}))
	assert.Equal(t, "Design", screens.TopicTitle(project.TopicRow{ID: 30, Title: "Design"}))
}
