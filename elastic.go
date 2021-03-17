package pecan

import (
	"context"
	"encoding/json"
	"github.com/olivere/elastic/v7"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Score           float64
	Id              string
	User            string   `json:"user,omitempty"`
	SubType         string   `json:"subtype,omitempty"`
	PreviousMessage *Message `json:"previous_message,omitempty"`
	SubMessage      *Message `json:"message,omitempty"`
	Channel         string   `json:"channel,omitempty"`
	EventTimestamp  string   `json:"event_ts,omitempty"`
	Timestamp       string   `json:"ts,omitempty"`
	Text            string   `json:"text,omitempty"`
}

type Conversation struct {
	Score    float64
	Messages []Message
}

// searchResponseToMessages maps responses from elasticsearch into slack messages,
// leaving slack ids for channels and names unresolved.
func searchResponseToMessages(resp *elastic.SearchResult) ([]Message, error) {
	if resp == nil {
		messages := make([]Message, 0)
		return messages, nil
	}
	messages := make([]Message, len(resp.Hits.Hits))
	for i, hit := range resp.Hits.Hits {
		b, err := hit.Source.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var msg Message
		msg.Id = hit.Id
		err = json.Unmarshal(b, &msg)
		if err != nil {
			return nil, err
		}
		// Parse the timestamp into something more readable.
		t := strings.Split(msg.EventTimestamp, ".")
		sec, err := strconv.Atoi(t[0])
		if err != nil {
			return nil, err
		}
		nsec, err := strconv.Atoi(t[1])
		if err != nil {
			return nil, err
		}

		msg.EventTimestamp = time.Unix(int64(sec), int64(nsec)).Format(time.RFC822)

		messages[i] = msg
		if hit.Score != nil { // Check if it is nil to prevent nil pointer dereference.
			messages[i].Score = *hit.Score
		}
	}
	return messages, nil

}

// buildChannelFilterQuery constructs an elasticsearch query that corresponds to a filter on selected channels.
func buildChannelFilterQuery(channels []string) []elastic.Query {
	filters := make([]elastic.Query, len(channels))
	for i := range channels {
		filters[i] = elastic.NewMatchQuery("channel", channels[i])
	}
	return filters
}

// queryMessages retrieves indexed messages using a search request.
func queryMessages(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) (*elastic.SearchResult, error) {
	return es.Search(request.Index).
		Query(elastic.NewBoolQuery().Must(
			elastic.NewMatchQuery("text", request.Query),
			elastic.NewRangeQuery("ts").Gte(request.From.Unix()).Lte(request.To.Add(24*time.Hour).Unix()),
			elastic.NewBoolQuery().Should(buildChannelFilterQuery(channels)...))).
		From(request.Start).
		Size(SearchSize).
		TrackScores(true).
		Sort("ts", false).
		Do(ctx)
}

// MoreMessages retrieves extra messages if required by the user
func MoreMessages(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([]Message, error) {
	var result []Message
	var searchresult *elastic.SearchResult
	var err error
	limit := 60
	t, err := strconv.ParseFloat(request.BaseMessageTime, 64)
	if err != nil {
		return nil, err
	}
	if request.PrevNext == 0 {
		for len(result) <= 6 && float64(limit) < t-float64(request.From.Unix()) {
			searchresult, err = es.Search(request.Index).
				Query(elastic.NewBoolQuery().Must(
					elastic.NewRangeQuery("ts").Gte(int64(t-float64(limit))).Lte(int64(t)),
					elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
				Size(SearchSize).
				Sort("ts", false).
				Do(ctx)
			result, err = searchResponseToMessages(searchresult)
			limit = limit * 2
			var temp []Message
			for i := range result {
				if result[i].Text != "" {
					temp = append(temp, result[i])
				}
			}
			result = temp
		}

		if result == nil {
			return nil, nil
		}

		if len(result) > 6 {
			result = result[1:6]
		} else {
			result = result[1:]
		}
		i := 0
		j := len(result) - 1
		for i < j {
			result[i], result[j] = result[j], result[i]
			i++
			j--
		}
	} else if request.PrevNext == 1 {
		for len(result) <= 6 && float64(limit) < float64(request.To.Unix())-t {
			searchresult, err = es.Search(request.Index).
				Query(elastic.NewBoolQuery().Must(
					elastic.NewRangeQuery("ts").Gte(int64(t)).Lte(int64(t+float64(limit))),
					elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
				Size(SearchSize).
				Sort("ts", true).
				Do(ctx)
			result, err = searchResponseToMessages(searchresult)
			limit = limit * 2
			var temp []Message
			for i := range result {
				if result[i].Text != "" {
					temp = append(temp, result[i])
				}
			}
			result = temp
		}

		if result == nil {
			return nil, nil
		}

		if len(result) > 6 {
			result = result[1:6]
		} else {
			result = result[1:]
		}

	}
	return result, err
}
