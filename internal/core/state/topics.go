package state

import "github.com/sorokin-vladimir/tele/internal/domain"

// ApplyTopicsPage records the first page of a forum's topics as Telegram
// listed it, and forgets the topics it reported deleted.
func (s *State) ApplyTopicsPage(chatID int64, topics []domain.Topic, deleted []int) (Change, bool) {
	s.st.SetTopicsPage(chatID, topics)
	s.st.RemoveTopics(chatID, deleted)
	c := Change{Kind: ChangeTopics, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyTopicRead records that a forum topic was read up to maxID.
func (s *State) ApplyTopicRead(chatID int64, topicID, maxID int) (Change, bool) {
	s.st.SetTopicRead(chatID, topicID, maxID)
	return s.topicsChanged(chatID)
}

// ApplyTopicMentionsRead clears a forum topic's unread mentions.
func (s *State) ApplyTopicMentionsRead(chatID int64, topicID int) (Change, bool) {
	s.st.SetTopicMentionsRead(chatID, topicID)
	return s.topicsChanged(chatID)
}

// ApplyTopicReactionsRead clears a forum topic's unread reactions.
func (s *State) ApplyTopicReactionsRead(chatID int64, topicID int) (Change, bool) {
	s.st.SetTopicReactionsRead(chatID, topicID)
	return s.topicsChanged(chatID)
}

// ApplyTopicMute records a forum topic's own notification setting and reports
// the one it replaced.
func (s *State) ApplyTopicMute(chatID int64, topicID int, mute domain.TopicMute) domain.TopicMute {
	was := s.st.SetTopicMute(chatID, topicID, mute)
	s.topicsChanged(chatID)
	return was
}

func (s *State) topicsChanged(chatID int64) (Change, bool) {
	c := Change{Kind: ChangeTopics, ChatID: chatID}
	s.commit(c)
	return c, true
}

// ApplyTopics records topics read again by id, and forgets the ones Telegram
// reported deleted.
func (s *State) ApplyTopics(chatID int64, topics []domain.Topic, deleted []int) (Change, bool) {
	s.st.UpdateTopics(chatID, topics)
	s.st.RemoveTopics(chatID, deleted)
	c := Change{Kind: ChangeTopics, ChatID: chatID}
	s.commit(c)
	return c, true
}
