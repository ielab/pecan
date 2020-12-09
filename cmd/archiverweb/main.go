package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/ielab/slackarchive"
	"github.com/nlopes/slack"
	"github.com/olivere/elastic/v7"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

func randState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func main() {
	config, err := slackarchive.NewConfig("config.json")
	if err != nil {
		panic(err)
	}

	api := slack.New(config.Slack.Token)

	ctx := context.Background()

	es, err := elastic.NewClient(elastic.SetURL(config.Elasticsearch.Url))
	if err != nil {
		panic(err)
	}

	tokens := make(map[string]string)

	router := gin.Default()
	router.LoadHTMLGlob("./web/*.html")
	router.Static("/static/", "./web/static")

	store := cookie.NewStore([]byte(config.Secrets.Cookie))
	router.Use(sessions.Sessions("slack-archive", store))

	// Middleware for redirecting for authentication.
	router.Use(func(c *gin.Context) {
		if strings.Contains(c.Request.URL.Path, "/login") {
			c.Next()
			return
		}
		session := sessions.Default(c)
		token := session.Get("token")
		if token == nil || len(token.(string)) == 0 {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		if accessToken, ok := tokens[token.(string)]; !ok {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		} else {
			if _, err = slack.New(accessToken).GetIMChannels(); err != nil {
				c.Redirect(http.StatusFound, "/login")
				c.Abort()
				return
			}
		}

		c.Next()
	})

	router.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)

		token := session.Get("token").(string)
		accessToken := tokens[token]

		// Default time values.
		from := "2010-01-01"
		to := time.Now().Format("2006-01-02")

		var (
			request      slackarchive.SearchRequest
			messages     []slack.Message
			responseType slackarchive.SearchResponseType
		)
		// If a query has been submitted, run a search.
		// Otherwise show recent messages.
		if err := c.ShouldBind(&request); err == nil && len(request.Query) > 0 {
			responseType = slackarchive.SEARCH
			messages, err = slackarchive.GetMessages(es, api, ctx, accessToken, request)
			if err != nil {
				panic(err)
			}
			from = request.From.Format(slackarchive.DateFormat)
			to = request.To.Format(slackarchive.DateFormat)
		} else {
			responseType = slackarchive.RECENT
			messages, err = slackarchive.GetRecentMessages(es, api, ctx, accessToken)
			if err != nil {
				panic(err)
			}
		}

		// Compute next and previous scrolls.
		next := request.Start + slackarchive.SearchSize
		prev := request.Start - slackarchive.SearchSize
		if next >= len(messages) {
			next = -1
		}
		if prev <= slackarchive.SearchSize {
			prev = -1
		}

		// Build the response.
		response := slackarchive.SearchResponse{
			Type:     responseType,
			Messages: messages,
			Query:    request.Query,
			From:     from,
			To:       to,

			Next: next,
			Prev: prev,
		}

		c.HTML(http.StatusOK, "index.html", response)
		return
	})
	router.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", nil)
		return
	})
	router.GET("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
	})
	router.GET("/login/oauth", func(c *gin.Context) {
		code := c.Query("code")
		accessToken, _, err := slack.GetOAuthToken(&http.Client{}, config.Slack.ClientId, config.Slack.ClientSecret, code, "")
		if err != nil {
			panic(err)
		}
		session := sessions.Default(c)
		token := randState()
		tokens[token] = accessToken
		session.Set("token", token)
		fmt.Println(accessToken)
		err = session.Save()
		if err != nil {
			panic(err)
		}

		c.Redirect(http.StatusFound, "/")
		return
	})

	log.Fatalln(router.Run(":4713"))

}
