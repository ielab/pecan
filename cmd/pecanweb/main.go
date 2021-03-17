package main

import (
	"context"
	"embed"
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/ielab/pecan"
	"github.com/olivere/elastic/v7"
	"html/template"
	"log"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed web/*
var webFS embed.FS

//go:embed web/static/*
var staticFS embed.FS

func main() {

	config, err := pecan.NewConfig("config.json")
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	var api pecan.ChatAPI
	switch config.API.Use {
	case "slack":
		api = pecan.NewSlackChatAPI(config)
	default:
		api = pecan.NewNoChatAPI()
	}

	es, err := elastic.NewClient(elastic.SetURL(config.Elasticsearch.Url), elastic.SetSniff(false))
	if err != nil {
		panic(err)
	}

	router := gin.Default()

	templates := template.Must(
		template.New("").
			Funcs(
				template.FuncMap{"add": func(a, b int) int {
					return a + b
				}}).
			ParseFS(webFS, "web/*.html"))
	router.SetHTMLTemplate(templates)

	router.GET("/static/*filepath", func(c *gin.Context) {
		c.FileFromFS(path.Join("/web/", c.Request.URL.Path), http.FS(staticFS))
	})
	

	// Middleware for redirecting for authentication.
	store := cookie.NewStore([]byte(config.Secrets.Cookie))
	router.Use(sessions.Sessions("pecan", store))

	router.Use(func(c *gin.Context) {
		if strings.Contains(c.Request.URL.Path, "/login") {
			c.Next()
			return
		}

		api.HandleAuthentication(c)

		c.Next()
	})

	router.GET("/", func(c *gin.Context) {

		// Default time values.
		from := "2010-01-01"
		to := time.Now().Format("2006-01-02")

		result, err := es.Count(config.Elasticsearch.Index).Do(ctx)
		if err != nil {
			panic(err)
		}
		// Build the response.
		response := pecan.StatisticsResponse{
			From:        from,
			To:          to,
			NumMessages: result,
		}
		c.HTML(http.StatusOK, "index.html", response)
		return
	})

	router.GET("/search", func(c *gin.Context) {
		// Default time values.
		from := "2010-01-01"
		to := time.Now().Format("2006-01-02")

		var (
			request       pecan.SearchRequest
			conversations []pecan.Conversation
		)
		// If a query has been submitted, run a search.
		// Otherwise show recent messages.
		if err := c.ShouldBind(&request); err == nil && len(request.Query) > 0 {
			// Determine which method should be used to search.
			conversations, err = pecan.GetConversations(c, es, ctx, request, api, pecan.TaskExecutor{
				// TODO: in future, this should be inferred from the request.
				AggregateConversations: pecan.TimeAggregator,
				ScoreConversations:     pecan.MessageScorer,
			})

			if err != nil {
				panic(err)
			}

			from = request.From.Format(pecan.DateFormat)
			to = request.To.Format(pecan.DateFormat)

		}

		// Compute next and previous scrolls.
		next := request.Start + pecan.SearchSize
		prev := request.Start - pecan.SearchSize

		if next >= len(conversations) {
			next = -1
		}
		if prev <= pecan.SearchSize {
			prev = -1
		}

		// Build the response.
		response := pecan.SearchResponse{
			Conversations: conversations,
			Query:         request.Query,
			From:          from,
			To:            to,
			Next:          next,
			Prev:          prev,
		}
		c.HTML(http.StatusOK, "search.html", response)
		return
	})
	//Page when the user requires to view extra messages
	router.POST("/more_messages", func(c *gin.Context) {
		var (
			request  pecan.SearchRequest
			messages []pecan.Message
		)
		if err := c.ShouldBind(&request); err == nil {
			var channel []string
			channel = append(channel, request.BaseMessageChannel)
			messages, err = pecan.MoreMessages(es, ctx, channel, request)
		}
		response := pecan.SearchResponse{
			Messages: messages,
			PrevNext: request.PrevNext,
			From:     request.From.Format(pecan.DateFormat),
			To:       request.To.Format(pecan.DateFormat),
		}
		c.HTML(http.StatusOK, "more_messages.html", response)
		return
	})
	router.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", config)
		return
	})
	router.GET("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
	})
	router.GET("/login/oauth", func(c *gin.Context) {
		api.HandleOAuth(c)
	})

	fmt.Print(`
	 ____  _____ ____    _    _   _ 
	|  _ \| ____/ ___|  / \  | \ | |
	| |_) |  _|| |     / _ \ |  \| |
	|  __/| |__| |___ / ___ \| |\  |
	|_|   |_____\____/_/   \_\_| \_|

	go -> http://localhost:4713

`)
	log.Fatalln(router.Run(":4713"))

}
