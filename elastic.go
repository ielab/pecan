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

//Return both messages and scores
func searchResponseToMessagesAndScores(resp *elastic.SearchResult) ([]Message, error) {
	if resp == nil {
		return nil, nil
	}
	messages := make([]Message, len(resp.Hits.Hits))
	for i, hit := range resp.Hits.Hits {
		b, err := resp.Hits.Hits[i].Source.MarshalJSON()
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

//query a conversation using a message
func queryConversation(es *elastic.Client, ctx context.Context, channels []string, TimeStamp string, request SearchRequest, message Message) []Message {
	var t float64
	var err error
	var result *elastic.SearchResult
	var left []Message
	var right []Message
	t, err = strconv.ParseFloat(TimeStamp, 64)

	if err != nil {
		panic(err)
	}
	lower := 60
	upper := 60
	for len(left) <= 6 && float64(lower) < t-float64(request.From.Unix()) {
		result, err = es.Search("slack-archive").
			Query(elastic.NewBoolQuery().Must(
				elastic.NewRangeQuery("ts").Gte(int64(t-float64(lower))).Lte(int64(t)),
				elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
			Size(SearchSize).
			Sort("ts", false).
			Do(ctx)
		left, err = searchResponseToMessages(result)
		lower = lower * 2
		var temp []Message
		for i := range left {
			if left[i].Text != "" {
				temp = append(temp, left[i])
			}
		}
		left = temp
	}
	var leftMessages []Message
	if len(left)>=6 {
		leftMessages = left[:6]
	}else{
		leftMessages = left[:len(left)]
	}
	leftMessages[0].Score = message.Score
	for len(right) <= 6 && float64(upper) < float64(request.To.Unix())-t {
		result, err = es.Search("slack-archive").
			Query(elastic.NewBoolQuery().Must(
				elastic.NewRangeQuery("ts").Gte(int64(t)).Lte(int64(t+float64(upper))),
				elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
			Size(SearchSize).
			Sort("ts", true).
			Do(ctx)
		right, err = searchResponseToMessages(result)
		upper = upper * 2
		var temp []Message
		for i := range right {
			if right[i].Text != "" {
				temp = append(temp, right[i])
			}
		}
		right = temp
	}
	var rightMessages []Message
	if len(right)>=6{
		for i := 5 ;i>0;i--{
			rightMessages = append(rightMessages,right[i])
		}
	}else{
		for i := len(right)-1;i>0;i--{
			rightMessages = append(rightMessages,right[i])
		}
	}
	conv := append(rightMessages,leftMessages...)
	return conv
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

// getRecentMessagesDev retrieves recent messages in a slice of specified channels.
// WARNING: Do not use this method in a production setting as it bypasses any authorisation checks.
func getRecentMessagesDev(es *elastic.Client, ctx context.Context, channels []string) ([]Message, error) {
	resp, err := queryRecentMessages(es, ctx, channels)
	if err != nil {
		return nil, err
	}

	return searchResponseToMessages(resp)
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

// getMoreMessagesDev retrieves extra messages if required by the user
func getMoreMessagesDev(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([]Message,error){
	var result []Message
	var searchresult *elastic.SearchResult
	var err error
	limit := 60
	t, err := strconv.ParseFloat(request.BaseMessageTime, 64)
	if request.PrevNext==0{
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
	}

	return result,err
}

func mergeConversations(conversations [][]Message)(mergedConversations [][]Message){

	mergedConversations = make([][]Message,0)
	channelIndex := make(map[string]int)
	for i := range conversations{
		if index,ok:= channelIndex[conversations[i][0].Channel] ;ok {
			if conversations[i][0].Timestamp >= mergedConversations[index][len(mergedConversations[index])-1].Timestamp {
				for j := range conversations[i] {
					if conversations[i][j].Timestamp < mergedConversations[index][len(mergedConversations[index])-1].Timestamp{
						mergedConversations[index] = append(mergedConversations[index],conversations[i][j])
					}else if conversations[i][j].Score>0{
						for k := range mergedConversations[index]{
							if mergedConversations[index][k].Timestamp == conversations[i][j].Timestamp && mergedConversations[index][k].Text==conversations[i][j].Text{
								mergedConversations[index][k] = conversations[i][j]
							}
						}
					}
				}
			}else{
				mergedConversations = append(mergedConversations,conversations[i])
				channelIndex[conversations[i][0].Channel] = len(mergedConversations)-1
			}
		}else{
			mergedConversations = append(mergedConversations,conversations[i])
			channelIndex[conversations[i][0].Channel] = len(mergedConversations)-1
		}
	}
	return mergedConversations
}

type internalConv struct{
	conversations [][]Message
	scores []float64
}

type sortByScore internalConv

func (sbs sortByScore) Len() int {
	return len(sbs.conversations)
}

func (sbs sortByScore) Swap(i,j int){
	sbs.conversations[i], sbs.conversations[j] = sbs.conversations[j], sbs.conversations[i]
	sbs.scores[i], sbs.scores[j] = sbs.scores[j], sbs.scores[i]
}

func (sbs sortByScore) Less(i,j int) bool {
	return sbs.scores[j] < sbs.scores[i]
}

func rankConversations(conversations [][]Message)(rankedConversations [][]Message){
	scores := make([]float64,len(conversations))
	for i:=range conversations{
		var score float64
		score = 0
		for j:=range conversations[i] {
			score += conversations[i][j].Score
		}
		scores[i]=score
	}
	combination := internalConv{conversations: conversations,scores: scores}
	sort.Sort(sortByScore(combination))
	return combination.conversations
}
// retrieves conversations
func getConversationsDev(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([][]Message, error) {
	resp, err := queryMessages(es, ctx, channels, request)
	var messages []Message
	messages, err = searchResponseToMessagesAndScores(resp)
	var conversations [][]Message

	for i := range messages {
		var (
			convChannel []string
			t           string
		)
		t = messages[i].Timestamp
		convChannel = append(convChannel, messages[i].Channel)
		conversation := queryConversation(es, ctx, convChannel, t, request,messages[i])
		conversations = append(conversations, conversation)
	}
	return rankConversations(mergeConversations(conversations)), err
}
