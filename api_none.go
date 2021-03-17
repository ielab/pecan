package pecan

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/olivere/elastic/v7"
)

type NoChatAPI struct {
}

func (api *NoChatAPI) GetMessages(es *elastic.Client, ctx context.Context, request SearchRequest) ([]Message, error) {
	resp, err := queryMessages(es, ctx, nil, request)
	if err != nil {
		return nil, err
	}
	return searchResponseToMessages(resp)
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
