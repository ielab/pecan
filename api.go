package pecan

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/olivere/elastic/v7"
)

type ChatAPI interface {
	GetMessages(es *elastic.Client, ctx context.Context, request SearchRequest) ([]Message, error)
	HandleOAuth(c *gin.Context)
	HandleAuthentication(c *gin.Context)
}
