package pecan

import (
	"github.com/gin-gonic/gin"
	"time"
)

const DateFormat = "2006-01-02"
const ElasticDateFormat = "yyyy-MM-dd"
const SearchSize = 50

type SearchResponseType int

const (
	RECENT SearchResponseType = iota
	SEARCH
)

type SearchResponse struct {
	Type          SearchResponseType
	Date          string
	Query         string
	From          string
	To            string
	Next          int
	Prev          int
	Took          time.Duration
	Messages      []Message
	Conversations []Conversation
	PrevNext      int
}

type StatisticsResponse struct {
	NumMessages int64
	From        string
	To          string
}

type SearchRequest struct {
	Query string    `form:"q" json:"query"`
	From  time.Time `form:"from" json:"from" time_format:"2006-01-02"`
	To    time.Time `form:"to" json:"to" time_format:"2006-01-02"`

	Start int `form:"start"`
	Next  int `form:"next"`
	Prev  int `form:"prev"`
	Page  int `form:"page"`

	PrevNext           int    `form:"prev_next"`
	BaseMessageTime    string `form:"base_message_time"`
	BaseMessageChannel string `form:"base_message_channel"`

	Index   string       `form:"-"`
	Context *gin.Context `form:"-"`
}
