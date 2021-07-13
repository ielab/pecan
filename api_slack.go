package pecan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/olivere/elastic/v7"
	"github.com/patrickmn/go-cache"
	"github.com/slack-go/slack"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type SlackChatAPI struct {
	client       *slack.Client
	clientId     string
	clientSecret string
	userCache    map[string]string
	channelCache map[string]string
	tokens       map[string]string
	idsCache     *cache.Cache
}

// searchResponseToMessagesUsingAPI maps responses from elasticsearch into slack messages
// using the slack API to resolve channel and user names.
func (api *SlackChatAPI) ConvertSearchResponseToMessages(resp *elastic.SearchResult) ([]Message, error) {
	messages := make([]Message, len(resp.Hits.Hits))
	for i, hit := range resp.Hits.Hits {
		b, err := resp.Hits.Hits[i].Source.MarshalJSON()
		if err != nil {
			return nil, err
		}

		var msg Message
		err = json.Unmarshal(b, &msg)
		if err != nil {
			return nil, err
		}

		// Grab the username from the API and assign it to the message.
		if len(msg.User) > 0 {
			u, err := api.LookupUsernameByID(msg.User)
			if err != nil {
				return nil, err
			}
			msg.User = u
		}

		if msg.PreviousMessage != nil {
			u, err := api.LookupUsernameByID(msg.PreviousMessage.User)
			if err != nil {
				return nil, err
			}
			msg.PreviousMessage.User = u
		}

		if msg.SubMessage != nil {
			u, err := api.LookupUsernameByID(msg.SubMessage.User)
			if err != nil {
				return nil, err
			}
			msg.SubMessage.User = u
		}

		// Grab the channel name from the API and assign it to the message.
		name, err := api.LookupGroupNameByID(msg.Channel)
		if err != nil {
			return nil, err
		}
		msg.ChannelName = name

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

// LookupUsernameByID retrieves the username for a slack user by their internal slack id.
func (api *SlackChatAPI) LookupUsernameByID(id string) (string, error) {
	if api.userCache == nil {
		api.userCache = make(map[string]string)
	}
	if name, ok := api.userCache[id]; ok {
		return name, nil
	}
	u, err := api.client.GetUserInfo(id)
	if err != nil {
		return id, nil
	}
	api.userCache[id] = u.Name
	return u.Name, nil
}

// LookupGroupNameByID retrieves the group name for a slack group by its internal slack id.
func (api *SlackChatAPI) LookupGroupNameByID(id string) (string, error) {
	if api.channelCache == nil {
		api.channelCache = make(map[string]string)
	}
	if name, ok := api.channelCache[id]; ok {
		return name, nil
	}

	// TODO Looks like this function is superseded by the one below? Need to double check.
	//g, err := api.client.GetGroupInfo(id)
	//if err == nil {
	//	api.channelCache[id] = g.Name
	//	return g.Name, nil
	//}

	c, err := api.client.GetConversationInfo(id, true)
	if err == nil {
		n := c.Name
		if len(c.Name) == 0 {
			n, err = api.LookupUsernameByID(c.User)
			if err != nil {
				return id, nil
			}
		}

		api.channelCache[id] = n
		return n, nil
	}

	u, err := api.LookupUsernameByID(id)
	if err == nil {
		return u, nil
	}

	return id, nil
}

// GetChannelsForUser retrieves the channels the user has permission to access.
func (api *SlackChatAPI) GetChannelsForUser(accessToken string) ([]string, error) {
	// Get the ids from a cache if they already exist.
	if api.idsCache == nil {
		api.idsCache = cache.New(5*time.Minute, 10*time.Minute)
	}
	if v, ok := api.idsCache.Get(accessToken); ok {
		return v.([]string), nil
	}

	api.client = slack.New(accessToken)
	// Private groups user has access to.
	groups, err := api.client.GetUserGroups()
	if err != nil {
		return nil, err
	}

	// Public conversations for all users.
	conversations, _, err := api.client.GetConversationsForUser(&slack.GetConversationsForUserParameters{})
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(groups)+len(conversations))
	for i := range groups {
		ids[i] = groups[i].ID
	}
	for i, j := len(groups), 0; i < len(groups)+len(conversations); i++ {
		ids[i] = conversations[j].ID
		j++
	}
	//TODO Maybe still need private chats? Need to double check.
	//for i, j := len(groups)+len(conversations), 0; i < len(ids); i++ {
	//	ids[i] = ims[j].ID
	//	j++
	//}

	api.idsCache.SetDefault(accessToken, ids)

	return ids, nil
}

// GetMessages uses the slack API to retrieve the channels an authenticated user has access to
// and then retrieves messages from these channels using a search request.
func (api *SlackChatAPI) GetMessages(es *elastic.Client, ctx context.Context, request SearchRequest) ([]Message, error) {
	session := sessions.Default(request.Context)
	token := api.tokens[session.Get("token").(string)]

	channels, err := api.GetChannelsForUser(token)
	if err != nil {
		return nil, err
	}

	resp, err := queryMessages(es, ctx, channels, request)
	if err != nil {
		return nil, err
	}
	return api.ConvertSearchResponseToMessages(resp)
}

func randState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func (api *SlackChatAPI) HandleOAuth(c *gin.Context) {
	code := c.Query("code")
	accessToken, _, err := slack.GetOAuthToken(&http.Client{}, api.clientId, api.clientSecret, code, "")
	if err != nil {
		panic(err)
	}
	session := sessions.Default(c)
	token := randState()
	api.tokens[token] = accessToken
	session.Set("token", token)
	err = session.Save()
	if err != nil {
		panic(err)
	}

	c.Redirect(http.StatusFound, "/")
	return
}

func (api *SlackChatAPI) HandleAuthentication(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("token")
	if token == nil || len(token.(string)) == 0 {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	}
	if accessToken, ok := api.tokens[token.(string)]; !ok {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
		return
	} else {
		if _, err := slack.New(accessToken).AuthTest(); err != nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
	}
}

func NewSlackChatAPI(config *Config) *SlackChatAPI {
	return &SlackChatAPI{
		client:       slack.New(config.API.Slack.Token),
		clientId:     config.API.Slack.ClientId,
		clientSecret: config.API.Slack.ClientSecret,
		userCache:    make(map[string]string),
		channelCache: make(map[string]string),
		tokens:       make(map[string]string),
		idsCache:     cache.New(cache.DefaultExpiration, cache.NoExpiration),
	}
}
