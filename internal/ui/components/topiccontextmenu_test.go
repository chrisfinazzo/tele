package components_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorokin-vladimir/tele/internal/core/project"
	"github.com/sorokin-vladimir/tele/internal/ui/components"
	"github.com/sorokin-vladimir/tele/internal/ui/keys"
)

// A topic's menu offers what can be done to a topic: read it and mute it. A
// topic has no unread mark, no folder and no archive (#275).
func TestTopicMenu_OffersReadAndMute(t *testing.T) {
	cm := components.NewTopicContextMenu(50, project.TopicRow{ID: 12, Unread: 2}, keys.DefaultKeyMap())

	v := cm.View()

	assert.Contains(t, v, "Mark as read")
	assert.Contains(t, v, "Mute")
	assert.NotContains(t, v, "Mark as unread")
	assert.NotContains(t, v, "Archive")
	assert.NotContains(t, v, "folder")
}

func TestTopicMenu_ARequestNamesTheTopic(t *testing.T) {
	cm := components.NewTopicContextMenu(50, project.TopicRow{ID: 12, Unread: 2, Muted: true}, keys.DefaultKeyMap())

	_, cmd := cm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, components.ToggleUnreadRequest{ChatID: 50, TopicID: 12, Unread: false}, cmd())

	cm = components.NewTopicContextMenu(50, project.TopicRow{ID: 12, Muted: true}, keys.DefaultKeyMap())
	_, cmd = cm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, components.ToggleMuteRequest{ChatID: 50, TopicID: 12, Muted: false}, cmd())
}
