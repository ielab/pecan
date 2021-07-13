package pecan

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/olivere/elastic/v7"
	"strconv"
	"strings"
	"time"
)

type NoChatAPI struct {
}

// searchResponseToMessages maps responses from elasticsearch into slack messages,
// leaving slack ids for channels and names unresolved.
func (api *NoChatAPI) ConvertSearchResponseToMessages(resp *elastic.SearchResult) ([]Message, error) {
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
		msg.ChannelName = msg.Channel // No human-readable channel name available.
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

func (api *NoChatAPI) GetMessages(es *elastic.Client, ctx context.Context, request SearchRequest) ([]Message, error) {
	resp, err := queryMessages(es, ctx, nil, request)
	if err != nil {
		return nil, err
	}
	return api.ConvertSearchResponseToMessages(resp)
}

func (api *NoChatAPI) HandleOAuth(c *gin.Context) {
	return
}

func (api *NoChatAPI) HandleAuthentication(c *gin.Context) {
	return
}

func NewNoChatAPI() *NoChatAPI {
	return &NoChatAPI{}
}
