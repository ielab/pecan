package slackarchive

import (
	"github.com/nlopes/slack"
	"github.com/patrickmn/go-cache"
	"time"
)

var userCache map[string]string
var channelCache map[string]string
var idsCache *cache.Cache

// LookupUsernameByID retrieves the username for a slack user by their internal slack id.
func LookupUsernameByID(api *slack.Client, id string) (string, error) {
	if userCache == nil {
		userCache = make(map[string]string)
	}
	if name, ok := userCache[id]; ok {
		return name, nil
	}
	u, err := api.GetUserInfo(id)
	if err != nil {
		return id, nil
	}
	userCache[id] = u.Name
	return u.Name, nil
}

// LookupGroupNameByID retrieves the group name for a slack group by its internal slack id.
func LookupGroupNameByID(api *slack.Client, id string) (string, error) {
	if channelCache == nil {
		channelCache = make(map[string]string)
	}
	if name, ok := channelCache[id]; ok {
		return name, nil
	}
	g, err := api.GetGroupInfo(id)
	if err == nil {
		channelCache[id] = g.Name
		return g.Name, nil
	}

	c, err := api.GetConversationInfo(id, true)
	if err == nil {
		n := c.Name
		if len(c.Name) == 0 {
			n, err = LookupUsernameByID(api, c.User)
			if err != nil {
				return id, nil
			}
		}

		channelCache[id] = n
		return n, nil
	}

	u, err := LookupUsernameByID(api, id)
	if err == nil {
		return u, nil
	}

	return id, nil
}

// GetChannelsForUser retrieves the channels the user has permission to access.
func GetChannelsForUser(accessToken string) ([]string, error) {
	// Get the ids from a cache if they already exist.
	if idsCache == nil {
		idsCache = cache.New(5*time.Minute, 10*time.Minute)
	}
	if v, ok := idsCache.Get(accessToken); ok {
		return v.([]string), nil
	}

	api := slack.New(accessToken)
	// Private groups user has access to.
	groups, err := api.GetGroups(false)
	if err != nil {
		return nil, err
	}

	// Public conversations for all users.
	conversations, _, err := api.GetConversationsForUser(&slack.GetConversationsForUserParameters{})
	if err != nil {
		return nil, err
	}

	// Direct message channels for the calling user.
	ims, err := api.GetIMChannels()
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(groups)+len(conversations)+len(ims))
	for i := range groups {
		ids[i] = groups[i].ID
	}
	for i, j := len(groups), 0; i < len(groups)+len(conversations); i++ {
		ids[i] = conversations[j].ID
		j++
	}
	for i, j := len(groups)+len(conversations), 0; i < len(ids); i++ {
		ids[i] = ims[j].ID
		j++
	}

	idsCache.SetDefault(accessToken, ids)

	return ids, nil
}
