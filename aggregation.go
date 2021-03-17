package pecan

type AggregateFunc func(conversations []Conversation) ([]Conversation, error)

// mergeConversations merge conversations that are overlapping with each other.
// At the same time, save the messages with positive scores to the merged conversation
func TimeAggregator(conversations []Conversation) ([]Conversation, error) {
	mergedConversations := make([]Conversation, 0)
	channelIndex := make(map[string]int)
	for i := range conversations {
		if len(conversations[i].Messages) == 0 {
			continue
		}
		if index, ok := channelIndex[conversations[i].Messages[0].Channel]; ok {
			if conversations[i].Messages[len(conversations[i].Messages)-1].Timestamp >= mergedConversations[index].Messages[0].Timestamp {
				for j := range conversations[i].Messages {
					if conversations[i].Messages[j].Timestamp < mergedConversations[index].Messages[0].Timestamp {
						mergedConversations[index].Messages = append([]Message{conversations[i].Messages[j]}, mergedConversations[index].Messages...)
					} else if conversations[i].Messages[j].Score > 0 {
						for k := range mergedConversations[index].Messages {
							if mergedConversations[index].Messages[k].Timestamp == conversations[i].Messages[j].Timestamp && mergedConversations[index].Messages[k].Text == conversations[i].Messages[j].Text {
								mergedConversations[index].Messages[k] = conversations[i].Messages[j]
							}
						}
					}
				}
			} else {
				mergedConversations = append(mergedConversations, conversations[i])
				channelIndex[conversations[i].Messages[0].Channel] = len(mergedConversations) - 1
			}
		} else {
			mergedConversations = append(mergedConversations, conversations[i])
			channelIndex[conversations[i].Messages[0].Channel] = len(mergedConversations) - 1
		}
	}
	return mergedConversations, nil
}
