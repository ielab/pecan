package pecan

import (
	"context"
	"github.com/olivere/elastic/v7"
	"strconv"
)

type BoundsFunc func(es *elastic.Client, api ChatAPI, ctx context.Context, channel string, message Message, request SearchRequest) ([]Message, error)

// TimeBounder retrieves messages in a conversation based on original messages
func TimeBounder(es *elastic.Client, api ChatAPI, ctx context.Context, channel string, message Message, request SearchRequest) ([]Message, error) {
	t, err := strconv.ParseFloat(message.Timestamp, 64)
	if err != nil {
		return nil, err
	}
	left, err := es.Search(request.Index).
		Query(elastic.NewBoolQuery().Must(
			elastic.NewRangeQuery("ts").Lte(int64(t)),
			elastic.NewBoolQuery().Must(buildChannelFilterQuery([]string{channel})...))).
		Size(6).
		Sort("ts", false).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	leftMessages, err := api.ConvertSearchResponseToMessages(left)
	if err != nil {
		return nil, err
	}
	if len(leftMessages) > 0 {
		leftMessages[0].Score = message.Score
	}

	right, err := es.Search(request.Index).
		Query(elastic.NewBoolQuery().Must(
			elastic.NewRangeQuery("ts").Gt(int64(t)),
			elastic.NewBoolQuery().Must(buildChannelFilterQuery([]string{channel})...))).
		Size(5).
		Sort("ts", true).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	rightMessages, err := api.ConvertSearchResponseToMessages(right)
	if err != nil {
		return nil, err
	}

	// Reverse the leftMessages slice.
	for i, j := 0, len(leftMessages)-1; j > i; i, j = i+1, j-1 {
		leftMessages[i], leftMessages[j] = leftMessages[j], leftMessages[i]
	}

	//return leftMessages, nil
	return append(leftMessages, rightMessages...), nil
}
