package slackarchive

import (
	"context"
	"encoding/json"
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
func queryConversation(es *elastic.Client, ctx context.Context, channels []string,TimeStamp string) (slack.Message, *elastic.SearchResult) {
	var t float64
	var err error
	var result *elastic.SearchResult
	var left []slack.Message
	var right []slack.Message
	t,err = strconv.ParseFloat(TimeStamp,64)

	if err != nil {
		panic(err)
	}
	var lower int64
	var upper int64
	lower = 60
	upper = 60
	var match slack.Message
	for len(left) <= 6 && lower < 86400{
		result, err = es.Search("slack-archive").
			Query(elastic.NewBoolQuery().Must(
				elastic.NewRangeQuery("ts").Gte(int64(t)-lower).Lte(int64(t)),
				elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
			Size(SearchSize).
			Sort("ts", false).
			Do(ctx)
		left,err = searchResponseToMessages(result)
		lower = lower*2
	}
	for len(right) <= 6 && upper < 86400 {
		result, err = es.Search("slack-archive").
			Query(elastic.NewBoolQuery().Must(
				elastic.NewRangeQuery("ts").Gte(int64(t)).Lte(int64(t)+upper),
				elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
			Size(SearchSize).
			Sort("ts", false).
			Do(ctx)
		right,err = searchResponseToMessages(result)
		upper = upper*2
	}
	match = left[0]
	conv,err := es.Search("slack-archive").
		Query(elastic.NewBoolQuery().Must(
			elastic.NewRangeQuery("ts").Gte(int64(t)-lower/4).Lte(int64(t)+upper/4),
			elastic.NewBoolQuery().Must(buildChannelFilterQuery(channels)...))).
		Size(SearchSize).
		Sort("ts", false).Do(ctx)
	return match,conv
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
func getConversationsDev(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([]slack.Message,[][]slack.Message, error) {
	resp, err := queryMessages(es, ctx, channels, request)
	var messages []slack.Message
	messages,err = searchResponseToMessages(resp)
	var conversations [][]slack.Message
	var matches []slack.Message
	for i := range messages{
		var (
			convChannel []string
			t           string
		)
		t = messages[i].Timestamp
		convChannel = append(convChannel,messages[i].Channel)
		match,respConv := queryConversation(es,ctx, convChannel,t)
		var conversation []slack.Message
		conversation,err = searchResponseToMessages(respConv)
		matches = append(matches,match)
		conversations = append(conversations,conversation)
	}
	return matches,conversations,err
}
