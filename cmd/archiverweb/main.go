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
	config, err := slackarchive.NewConfig("config.sample.json")
	if err != nil {
		panic(err)
	}

	api := slack.New(config.Slack.Token)

	ctx := context.Background()

	es, err := elastic.NewClient(elastic.SetURL(config.Elasticsearch.Url),elastic.SetSniff(false))
	if err != nil {
		panic(err)
	}

	tokens := make(map[string]string)

	router := gin.Default()
	router.LoadHTMLGlob("./web/*.html")
	router.Static("/static/", "./web/static")

	// Middleware for redirecting for authentication.
	if !config.Options.DevEnvironment { // Bypass this if we are in the dev environment.
		store := cookie.NewStore([]byte(config.Secrets.Cookie))
		router.Use(sessions.Sessions("slack-archive", store))

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
	}

	router.GET("/", func(c *gin.Context) {

		// If production environment, get the access token of the user.
		var accessToken string
		if !config.Options.DevEnvironment {
			session := sessions.Default(c)

			token := session.Get("token").(string)
			accessToken = tokens[token]
		}
		page := 1
		// Default time values.
		from := "2010-01-01"
		to := time.Now().Format("2006-01-02")

		var (
			request      slackarchive.SearchRequest
			messages     []slack.Message
			responseType slackarchive.SearchResponseType
			conversations [][]slack.Message
		)
		// If a query has been submitted, run a search.
		// Otherwise show recent messages.
		if err := c.ShouldBind(&request); err == nil && len(request.Query) > 0 {
			responseType = slackarchive.SEARCH

			// Determine which method should be used to search.
			if config.Options.DevEnvironment {
				messages, err = config.DevEnvironment.DevGetMessages(es, ctx, config.Options.DevChannels, request)

				conversations ,err = config.DevEnvironment.DevGetConversations(es, ctx, config.Options.DevChannels, request)

				/*
				var clusters []string
				for i := range conversations{
					var arg string
					arg = ""
					for j := range conversations[i]{
						arg = arg + conversations[i][j].Text+"§"
					}
					cmd := exec.Command("python", "../python/cluster.py",arg)
					//fmt.Println(arg)
					var out []byte
					out,err = cmd.CombinedOutput()
					fmt.Println(string(out))
					clusters= append(clusters, string(out))
					break
				}*/

			} else {
				messages, err = slackarchive.GetMessages(es, api, ctx, accessToken, request)
				if err != nil {
					panic(err)
				}
			}
			page = request.Page
			from = request.From.Format(slackarchive.DateFormat)
			to = request.To.Format(slackarchive.DateFormat)
		} else {
			responseType = slackarchive.RECENT

			// Determine which method should be used for recent messages.
			if config.Options.DevEnvironment {
				messages, err = config.DevEnvironment.DevGetRecentMessages(es, ctx, config.Options.DevChannels)
				conversations = append(conversations, messages)
				if err != nil {
					panic(err)
				}
			} else {
				messages, err = slackarchive.GetRecentMessages(es, api, ctx, accessToken)
				if err != nil {
					panic(err)
				}
			}

		}

		// Compute next and previous scrolls.
		next := request.Start + slackarchive.SearchSize
		prev := request.Start - slackarchive.SearchSize
		prevpage := page-1
		nextpage := page+1
		if prevpage==0{
			prevpage=1
		}
		if nextpage>len(conversations){
			nextpage = len(conversations)
		}
		if next >= len(messages) {
			next = -1
		}
		if prev <= slackarchive.SearchSize {
			prev = -1
		}

		// Build the response.
		response := slackarchive.SearchResponse{
			Type:     responseType,
			Messages: conversations[page-1],
			Query:    request.Query,
			From:     from,
			To:       to,
			Next: next,
			Page: page,
			PrevPage: prevpage,
			NextPage: nextpage,
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

	fmt.Print(`
       .__                 __                         .__    .__              
  _____|  | _____    ____ |  | _______ _______   ____ |  |__ |__|__  __ ____  
 /  ___/  | \__  \ _/ ___\|  |/ /\__  \\_  __ \_/ ___\|  |  \|  \  \/ // __ \ 
 \___ \|  |__/ __ \\  \___|    <  / __ \|  | \/\  \___|   Y  \  |\   /\  ___/ 
/____  >____(____  /\___  >__|_ \(____  /__|    \___  >___|  /__| \_/  \___  >
     \/          \/     \/     \/     \/            \/     \/              \/ 

	go -> http://localhost:4713

`)
	log.Fatalln(router.Run(":4713"))

}
