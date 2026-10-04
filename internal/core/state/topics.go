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

// ApplyTopics records topics read again by id, and forgets the ones Telegram
// reported deleted.
func (s *State) ApplyTopics(chatID int64, topics []domain.Topic, deleted []int) (Change, bool) {
	s.st.UpdateTopics(chatID, topics)
	s.st.RemoveTopics(chatID, deleted)
	c := Change{Kind: ChangeTopics, ChatID: chatID}
	s.commit(c)
	return c, true
}
