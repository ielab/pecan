package slackarchive

import (
	"github.com/nlopes/slack"
	"time"
)

const DateFormat = "2006-01-02"
const ElasticDateFormat = "yyyy-MM-dd"
const SearchSize = 10

type SearchResponseType int

const (
	RECENT SearchResponseType = iota
	SEARCH
)

type SearchResponse struct {
	Type     SearchResponseType
	Messages []slack.Message
	Date     string

	Query     string
	From      string
	To        string
	Next      int
	Prev      int
	Took      time.Duration
	Page      int
	PrevPage  int
	NextPage  int
}

type SearchRequest struct {
	Query string    `form:"q"`
	From  time.Time `form:"from" time_format:"2006-01-02"`
	To    time.Time `form:"to" time_format:"2006-01-02"`

	Start int `form:"start"`
	Next  int `form:"next"`
	Prev  int `form:"prev"`
	Page  int `form:"page"`
}
