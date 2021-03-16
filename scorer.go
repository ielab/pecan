package pecan

type ScoreConversations func(conversations []Conversation) ([]Conversation, error)

func MessageScorer(conversations []Conversation) ([]Conversation, error) {
	for i := range conversations {
		var score float64
		score = 0
		for j := range conversations[i].Messages {
			score += conversations[i].Messages[j].Score
		}
		conversations[i].Score = score
	}
	return conversations, nil
}

type internalConv struct {
	conversations []Conversation
	scores        []float64
}

type sortByScore internalConv

func (sbs sortByScore) Len() int {
	return len(sbs.conversations)
}

func (sbs sortByScore) Swap(i, j int) {
	sbs.conversations[i], sbs.conversations[j] = sbs.conversations[j], sbs.conversations[i]
	sbs.scores[i], sbs.scores[j] = sbs.scores[j], sbs.scores[i]
}

func (sbs sortByScore) Less(i, j int) bool {
	return sbs.scores[j] < sbs.scores[i]
}
