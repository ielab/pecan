package slackarchive

import (
	"context"
	"encoding/json"
	"github.com/nlopes/slack"
	"github.com/olivere/elastic/v7"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Score float64
	slack.Message
}

type Conversation []Message

// searchResponseToMessagesUsingAPI maps responses from elasticsearch into slack messages
// using the slack API to resolve channel and user names.
func searchResponseToMessagesUsingAPI(resp *elastic.SearchResult, api *slack.Client) ([]Message, error) {
	messages := make([]Message, len(resp.Hits.Hits))
	for i, hit := range resp.Hits.Hits {
		b, err := resp.Hits.Hits[i].Source.MarshalJSON()
		if err != nil {
			return nil, err
		}

		var msg Message
		err = json.Unmarshal(b, &msg)
		if err != nil {
			return nil, err
		}

		// Grab the username from the API and assign it to the message.
		if len(msg.User) > 0 {
			u, err := LookupUsernameByID(api, msg.User)
			if err != nil {
				return nil, err
			}
			msg.User = u
		}

		if msg.PreviousMessage != nil {
			u, err := LookupUsernameByID(api, msg.PreviousMessage.User)
			if err != nil {
				return nil, err
			}
			msg.PreviousMessage.User = u
		}

		if msg.SubMessage != nil {
			u, err := LookupUsernameByID(api, msg.SubMessage.User)
			if err != nil {
				return nil, err
			}
			msg.SubMessage.User = u
		}

		// Grab the channel name from the API and assign it to the message.
		name, err := LookupGroupNameByID(api, msg.Channel)
		if err != nil {
			return nil, err
		}
		msg.Channel = name

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

		var msg slack.Message
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

		messages[i].Message = msg
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
func queryRecentMessages(es *elastic.Client, ctx context.Context, channels []string) (*elastic.SearchResult, error) {
	filters := buildChannelFilterQuery(channels)

	return es.Search("slack-archive").
		Query(elastic.NewBoolQuery().Should(filters...)).
		Sort("ts", false).
		Size(SearchSize).
		Do(ctx)
}

// queryMessages retrieves indexed messages using a search request.
func queryMessages(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) (*elastic.SearchResult, error) {
	return es.Search("slack-archive").
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
func queryConversation(es *elastic.Client, ctx context.Context, channels []string, message Message) ([]Message, error) {
	t, err := strconv.ParseFloat(message.Timestamp, 64)
	if err != nil {
		return nil, err
	}
	left, err := es.Search("slack-archive").
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

	right, err := es.Search("slack-archive").
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

// GetMessages uses the slack API to retrieve the channels an authenticated user has access to
// and then retrieves messages from these channels using a search request.
func GetMessages(es *elastic.Client, api *slack.Client, ctx context.Context, accessToken string, request SearchRequest) ([]Message, error) {
	channels, err := GetChannelsForUser(accessToken)
	if err != nil {
		return nil, err
	}

	resp, err := queryMessages(es, ctx, channels, request)
	if err != nil {
		return nil, err
	}
	return searchResponseToMessagesUsingAPI(resp, api)
}

// GetRecentMessages uses the slack API to retrieve the channels an authenticated user has access to
// and then retrieves recent messages from these channels.
func GetRecentMessages(es *elastic.Client, api *slack.Client, ctx context.Context, accessToken string) ([]Message, error) {
	channels, err := GetChannelsForUser(accessToken)
	if err != nil {
		return nil, err
	}

	resp, err := queryRecentMessages(es, ctx, channels)
	if err != nil {
		return nil, err
	}

	return searchResponseToMessagesUsingAPI(resp, api)
}

// GetConversations uses the slack API to retrieve the channels an authenticated user has access to
// and then retrieves conversations from these channels using a search request.
func GetConversations(es *elastic.Client, api *slack.Client, ctx context.Context, accessToken string, request SearchRequest) ([]Conversation, error) {
	messages, err := GetMessages(es, api, ctx, accessToken, request)
	if err != nil {
		return nil, err
	}
	return createConversations(es, ctx, request, messages)
}

// GetRecentConversations uses the slack API to retrieve the channels an authenticated user has access to
// and then retrieves recent messages from these channels.
func GetRecentConversations(es *elastic.Client, api *slack.Client, ctx context.Context, accessToken string) ([]Conversation, error) {
	messages, err := GetRecentMessages(es, api, ctx, accessToken)
	if err != nil {
		return nil, err
	}
	return createConversations(es, ctx, SearchRequest{}, messages)
}

// getMessagesDev retrieves messages in a slice of specified channels using a search request.
// WARNING: Do not use this method in a production setting as it bypasses any authorisation checks.
func getMessagesDev(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([]Message, error) {
	resp, err := queryMessages(es, ctx, channels, request)
	if err != nil {
		return nil, err
	}
	return searchResponseToMessages(resp)
}

// getRecentMessagesDev retrieves recent messages in a slice of specified channels.
// WARNING: Do not use this method in a production setting as it bypasses any authorisation checks.
func getRecentMessagesDev(es *elastic.Client, ctx context.Context, channels []string) ([]Message, error) {
	resp, err := queryRecentMessages(es, ctx, channels)
	if err != nil {
		return nil, err
	}

	return searchResponseToMessages(resp)
}

// getConversationsDev retrieves conversations based on the retrieved messages.
// WARNING: Do not use this method in a production setting as it bypasses any authorisation checks.
func getConversationsDev(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([]Conversation, error) {
	messages, err := getMessagesDev(es, ctx, channels, request)
	if err != nil {
		return nil, err
	}
	return createConversations(es, ctx, request, messages)
}

// getConversationsDev retrieves conversations based on the retrieved messages.
// WARNING: Do not use this method in a production setting as it bypasses any authorisation checks.
func getRecentConversationsDev(es *elastic.Client, ctx context.Context, channels []string) ([]Conversation, error) {
	messages, err := getRecentMessagesDev(es, ctx, channels)
	if err != nil {
		return nil, err
	}
	return createConversations(es, ctx, SearchRequest{}, messages)
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
			searchresult, err = es.Search("slack-archive").
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
			searchresult, err = es.Search("slack-archive").
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
		if len(result) > 6 {
			result = result[1:6]
		} else {
			result = result[1:]
		}

	}
	return result, err
}

// mergeConversations merge conversations that are overlapping with each other.
// At the same time, save the messages with positive scores to the merged conversation
func mergeConversations(conversations []Conversation) (mergedConversations []Conversation) {

	mergedConversations = make([]Conversation, 0)
	channelIndex := make(map[string]int)
	for i := range conversations {
		if len(conversations[i]) == 0 {
			continue
		}
		if index, ok := channelIndex[conversations[i][0].Channel]; ok {
			if conversations[i][0].Timestamp >= mergedConversations[index][len(mergedConversations[index])-1].Timestamp {
				for j := range conversations[i] {
					if conversations[i][j].Timestamp < mergedConversations[index][len(mergedConversations[index])-1].Timestamp {
						mergedConversations[index] = append(mergedConversations[index], conversations[i][j])
					} else if conversations[i][j].Score > 0 {
						for k := range mergedConversations[index] {
							if mergedConversations[index][k].Timestamp == conversations[i][j].Timestamp && mergedConversations[index][k].Text == conversations[i][j].Text {
								mergedConversations[index][k] = conversations[i][j]
							}
						}
					}
				}
			} else {
				mergedConversations = append(mergedConversations, conversations[i])
				channelIndex[conversations[i][0].Channel] = len(mergedConversations) - 1
			}
		} else {
			mergedConversations = append(mergedConversations, conversations[i])
			channelIndex[conversations[i][0].Channel] = len(mergedConversations) - 1
		}
	}
	return mergedConversations
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

// rankConversations rank conversations based on sum of scores.
func rankConversations(conversations []Conversation) (rankedConversations []Conversation) {
	scores := make([]float64, len(conversations))
	for i := range conversations {
		var score float64
		score = 0
		for j := range conversations[i] {
			score += conversations[i][j].Score
		}
		scores[i] = score
	}
	combination := internalConv{conversations: conversations, scores: scores}
	sort.Sort(sortByScore(combination))
	return combination.conversations
}

// createConversations retrieves conversations based on the retrieved messages
func createConversations(es *elastic.Client, ctx context.Context, request SearchRequest, messages []Message) ([]Conversation, error) {
	var conversations []Conversation
	for i := range messages {
		conversation, err := queryConversation(es, ctx, []string{messages[i].Channel}, messages[i])
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return rankConversations(mergeConversations(conversations)), nil
	//return rankConversations(conversations), nil
}
