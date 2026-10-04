package project

// Delta is one addressed update for one subscription. Exactly one payload field
// is set, matching the window the subscription was created with.
type Delta struct {
	Sub      SubID
	ChatList *ChatListDelta
	History  *HistoryDelta
}
