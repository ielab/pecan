package pecan

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/olivere/elastic/v7"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Score           float64
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

// queryRecentMessages retrieves a top-k set of recent messages given a slice of channels.
func queryRecentMessages(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) (*elastic.SearchResult, error) {
	filters := buildChannelFilterQuery(channels)

	return es.Search(request.Index).
		Query(elastic.NewBoolQuery().Should(filters...)).
		Sort("ts", false).
		Size(SearchSize).
		Do(ctx)
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

// queryConversation retrieves messages in a conversation based on original messages
func queryConversation(es *elastic.Client, ctx context.Context, channels []string, message Message, request SearchRequest) ([]Message, error) {
	t, err := strconv.ParseFloat(message.Timestamp, 64)
	if err != nil {
		return nil, err
	}
	left, err := es.Search(request.Index).
		Query(elastic.NewBoolQuery().Must(
			elastic.NewRangeQuery("ts").Lte(int64(t)),
			elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
		Size(6).
		Sort("ts", false).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	leftMessages, err := searchResponseToMessages(left)
	if err != nil {
		return nil, err
	}
	if len(leftMessages) > 0 {
		leftMessages[0].Score = message.Score
	}

	right, err := es.Search(request.Index).
		Query(elastic.NewBoolQuery().Must(
			elastic.NewRangeQuery("ts").Gt(int64(t)),
			elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
		Size(5).
		Sort("ts", true).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	rightMessages, err := searchResponseToMessages(right)
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

// GetConversations uses the slack API to retrieve the channels an authenticated user has access to
// and then retrieves conversations from these channels using a search request.
func GetConversations(c *gin.Context, es *elastic.Client, ctx context.Context, request SearchRequest, api ChatAPI, exec TaskExecutor) ([]Conversation, error) {
	messages, err := api.GetMessages(c, es, ctx, request)
	if err != nil {
		return nil, err
	}
	return createConversations(es, ctx, messages, exec, request)
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

// createConversations retrieves conversations based on the retrieved messages.
// This is the main method that should be used to retrieve conversations.
func createConversations(es *elastic.Client, ctx context.Context, messages []Message, exec TaskExecutor, request SearchRequest) ([]Conversation, error) {
	var conversations []Conversation
	for i := range messages {
		conversation, err := queryConversation(es, ctx, []string{messages[i].Channel}, messages[i], request)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, Conversation{
			Score:    0,
			Messages: conversation,
		})
	}

	merged, err := exec.AggregateConversations(conversations)
	if err != nil {
		return nil, err
	}

	scored, err := exec.ScoreConversations(merged)
	if err != nil {
		return nil, err
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored, nil

}
