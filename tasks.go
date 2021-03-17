package pecan

import (
	"context"
	"github.com/olivere/elastic/v7"
	"sort"
)

type TaskExecutor struct {
	api ChatAPI
	es  *elastic.Client

	BoundsFunc
	AggregateFunc
	ScoreFunc
}

func NewTaskExecutor(api ChatAPI, es *elastic.Client) *TaskExecutor {
	return &TaskExecutor{
		api: api,
		es:  es,

		BoundsFunc:    TimeBounder,
		AggregateFunc: TimeAggregator,
		ScoreFunc:     MessageScorer,
	}
}

func (exec *TaskExecutor) SetBoundsFunc(boundsFunc BoundsFunc) *TaskExecutor {
	exec.BoundsFunc = boundsFunc
	return exec
}

func (exec *TaskExecutor) SetAggregateFunc(aggregateFunc AggregateFunc) *TaskExecutor {
	exec.AggregateFunc = aggregateFunc
	return exec
}

func (exec *TaskExecutor) SetScoreFunc(scoreFunc ScoreFunc) *TaskExecutor {
	exec.ScoreFunc = scoreFunc
	return exec
}

func (exec *TaskExecutor) GetMessages(ctx context.Context, request SearchRequest) ([]Message, error) {
	return exec.api.GetMessages(exec.es, ctx, request)
}

func (exec *TaskExecutor) GetConversations(ctx context.Context, request SearchRequest) ([]Conversation, error) {
	messages, err := exec.GetMessages(ctx, request)
	if err != nil {
		return nil, err
	}
	var conversations []Conversation
	for i := range messages {
		conversation, err := exec.BoundsFunc(exec.es, ctx, messages[i].Channel, messages[i], request)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, Conversation{
			Score:    0,
			Messages: conversation,
		})
	}

	merged, err := exec.AggregateFunc(conversations)
	if err != nil {
		return nil, err
	}

	scored, err := exec.ScoreFunc(merged)
	if err != nil {
		return nil, err
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored, nil
}

func MustMapBoundFunc(name string) BoundsFunc {
	switch name {
	default:
		return TimeBounder
	}
}

func MustMapAggregateFunc(name string) AggregateFunc {
	switch name {
	default:
		return TimeAggregator
	}
}
func MustMapScoreFunc(name string) ScoreFunc {
	switch name {
	default:
		return MessageScorer
	}
}
