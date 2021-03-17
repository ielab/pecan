package pecan

type ScoreFunc func(conversations []Conversation) ([]Conversation, error)

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
