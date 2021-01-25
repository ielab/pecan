package slackarchive

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nlopes/slack"
	"github.com/olivere/elastic/v7"
	"strconv"
	"strings"
	"time"
)

// searchResponseToMessagesUsingAPI maps responses from elasticsearch into slack messages
// using the slack API to resolve channel and user names.
func searchResponseToMessagesUsingAPI(resp *elastic.SearchResult, api *slack.Client) ([]slack.Message, error) {
	messages := make([]slack.Message, len(resp.Hits.Hits))
	for i := range resp.Hits.Hits {
		b, err := resp.Hits.Hits[i].Source.MarshalJSON()
		if err != nil {
			return nil, err
		}

		var msg slack.Message
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
	}

	return messages, nil
}

// searchResponseToMessages maps responses from elasticsearch into slack messages,
// leaving slack ids for channels and names unresolved.
func searchResponseToMessages(resp *elastic.SearchResult) ([]slack.Message, error) {
	if resp == nil{
		messages := make([]slack.Message, 0)
		return messages, nil
	}
	messages := make([]slack.Message, len(resp.Hits.Hits))
	for i := range resp.Hits.Hits {
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

		messages[i] = msg

	}
	return messages, nil

}

//Return both messages and scores
func searchResponseToMessagesAndScores(resp *elastic.SearchResult) ([]slack.Message, []float64,error) {
	if resp == nil{
		messages := make([]slack.Message, 0)
		scores := make([]float64,0)
		return messages,scores, nil
	}
	messages := make([]slack.Message, len(resp.Hits.Hits))
	scores := make([]float64,len(resp.Hits.Hits))
	for i := range resp.Hits.Hits {
		b, err := resp.Hits.Hits[i].Source.MarshalJSON()
		if err != nil {
			return nil,nil, err
		}

		var msg slack.Message
		var score float64
		err = json.Unmarshal(b, &msg)
		if err != nil {
			return nil,nil, err
		}
		if resp.Hits.Hits[i].Score != nil{ //Debug:Check if score is nil
			score = *resp.Hits.Hits[i].Score
			fmt.Println(score) //If score is not nil, print it out
		}
		// Parse the timestamp into something more readable.
		t := strings.Split(msg.EventTimestamp, ".")
		sec, err := strconv.Atoi(t[0])
		if err != nil {
			return nil, nil, err
		}
		nsec, err := strconv.Atoi(t[1])
		if err != nil {
			return nil, nil, err
		}

		msg.EventTimestamp = time.Unix(int64(sec), int64(nsec)).Format(time.RFC822)

		messages[i] = msg
		scores[i] = score
	}
	return messages, scores, nil

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
		Sort("ts", false).
		Do(ctx)
}

//query a conversation using a message
func queryConversation(es *elastic.Client, ctx context.Context, channels []string,TimeStamp string,request SearchRequest) ([]slack.Message) {
	var t float64
	var err error
	var result *elastic.SearchResult
	var left []slack.Message
	var right []slack.Message
	t,err = strconv.ParseFloat(TimeStamp,64)

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
		var temp []slack.Message
		for i := range left{
			if left[i].Text!=""{
				temp = append(temp,left[i])
			}
		}
		left = temp
	}

	for len(right) <= 6 && float64(upper) < float64(request.To.Unix())-t {
		result, err = es.Search("slack-archive").
			Query(elastic.NewBoolQuery().Must(
				elastic.NewRangeQuery("ts").Gte(int64(t)).Lte(int64(t+float64(upper))),
				elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
			Size(SearchSize).
			Sort("ts", false).
			Do(ctx)
		right,err = searchResponseToMessages(result)
		upper = upper*2
		var temp []slack.Message
		for i := range right{
			if right[i].Text!=""{
				temp = append(temp,right[i])
			}
		}
		right = temp
	}
	conv := append(right[len(right)-6:len(right)-1],left[:6]...)
	return conv
}
// GetMessages uses the slack API to retrieve the channels an authenticated user has access to
// and then retrieves messages from these channels using a search request.
func GetMessages(es *elastic.Client, api *slack.Client, ctx context.Context, accessToken string, request SearchRequest) ([]slack.Message, error) {
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
func GetRecentMessages(es *elastic.Client, api *slack.Client, ctx context.Context, accessToken string) ([]slack.Message, error) {
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
func getRecentMessagesDev(es *elastic.Client, ctx context.Context, channels []string) ([]slack.Message, error) {
	resp, err := queryRecentMessages(es, ctx, channels)
	if err != nil {
		return nil, err
	}

	return searchResponseToMessages(resp)
}

// getMessagesDev retrieves messages in a slice of specified channels using a search request.
// WARNING: Do not use this method in a production setting as it bypasses any authorisation checks.
func getMessagesDev(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([]slack.Message, error) {
	resp, err := queryMessages(es, ctx, channels, request)
	if err != nil {
		return nil, err
	}
	return searchResponseToMessages(resp)
}

// retrieves conversations
func getConversationsDev(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([][]slack.Message, error) {
	resp, err := queryMessages(es, ctx, channels, request)
	var messages []slack.Message
	var message_scores []float64
	messages,message_scores,err = searchResponseToMessagesAndScores(resp)
	var conversations [][]slack.Message
	for i := range messages{
		var (
			convChannel []string
			t           string
		)
		t = messages[i].Timestamp
		convChannel = append(convChannel,messages[i].Channel)
		conversation := queryConversation(es,ctx, convChannel,t,request)
		conversations = append(conversations,conversation)
		fmt.Println(message_scores[i]) //Print out the sore, 0 means SearchHit.Score is nil
	}
	return conversations,err
}
