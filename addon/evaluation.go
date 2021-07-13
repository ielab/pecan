package addon

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/hscells/trecresults"
	"github.com/ielab/pecan"
	"github.com/olivere/elastic/v7"
	"net/http"
	"strconv"
	"time"
)

type EvaluationAddon struct {
	es    *elastic.Client
	api   pecan.ChatAPI
	index string
}

type EvaluationRequest struct {
	Bounder    string `json:"bounder,omitempty"`
	Aggregator string `json:"aggregator,omitempty"`
	Scorer     string `json:"scorer,omitempty"`

	Topic string `json:"topic"`
	pecan.SearchRequest
}

func (addon *EvaluationAddon) messagesResults(request EvaluationRequest) (trecresults.ResultList, error) {
	exec := pecan.NewTaskExecutor(addon.api, addon.es).
		SetBoundsFunc(pecan.MustMapBoundFunc(request.Bounder)).
		SetAggregateFunc(pecan.MustMapAggregateFunc(request.Aggregator)).
		SetScoreFunc(pecan.MustMapScoreFunc(request.Scorer))

	conversations, err := exec.GetConversations(context.Background(), addon.api, request.SearchRequest)
	if err != nil {
		return nil, err
	}

	var numResults int
	for _, conversation := range conversations {
		numResults += len(conversation.Messages)
	}

	results := make(trecresults.ResultList, numResults)

	var i int64
	for cId, conversation := range conversations {
		cIdStr := strconv.Itoa(cId)
		for _, message := range conversation.Messages {
			results[i] = &trecresults.Result{
				Topic:     request.Topic,
				Iteration: "0",
				DocId:     cIdStr + "_" + message.Id,
				Rank:      i,
				Score:     conversation.Score,
				RunName:   "",
			}
			i++
		}
	}

	return results, nil
}

func NewEvaluationAddon() *EvaluationAddon {
	return &EvaluationAddon{}
}

func (addon *EvaluationAddon) Initialise(es *elastic.Client, api pecan.ChatAPI, config *pecan.Config) {
	addon.es = es
	addon.api = api
	addon.index = config.Elasticsearch.Index
}

func (addon *EvaluationAddon) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var request EvaluationRequest

		if err := c.BindJSON(&request); err == nil && len(request.Query) > 0 {
			from := "2010-01-01"
			to := time.Now().Format("2006-01-02")
			request.From, err = time.Parse(pecan.DateFormat, from)
			if err != nil {
				panic(err)
			}
			request.To, err = time.Parse(pecan.DateFormat, to)
			if err != nil {
				panic(err)
			}
			request.Index = addon.index
			results, err := addon.messagesResults(request)
			if err != nil {
				panic(err)
			}

			b, err := results.Marshal()
			if err != nil {
				panic(err)
			}

			c.Data(http.StatusOK, "text/plain", b)
		}

	}
}
