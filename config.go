package slackarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/olivere/elastic/v7"
	"io/ioutil"
)

// Config contains any and all configuration items
// for the proper functioning of this application.
type Config struct {
	Slack struct {
		Token             string `json:"token"`
		VerificationToken string `json:"verification_token"`
		ClientId          string `json:"client_id"`
		ClientSecret      string `json:"client_secret"`
	} `json:"slack"`
	Elasticsearch struct {
		Login struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"login"`
		Index string `json:"index"`
		Url   string `json:"url"`
	} `json:"elasticsearch"`
	Secrets struct {
		Cookie string `json:"cookie"`
	} `json:"secrets"`
	Options struct {
		DevEnvironment bool     `json:"dev_environment"`
		DevChannels    []string `json:"dev_channels"`
	}
	DevEnvironment struct {
		DevGetMessages       func(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([]Message, error)
		DevGetRecentMessages func(es *elastic.Client, ctx context.Context, channels []string) ([]Message, error)
		DevGetConversations	func(es *elastic.Client, ctx context.Context, channels []string, request SearchRequest) ([][]Message, error)
	}
}

// NewConfig creates a new config that can be used, as read
// by the file specified by path.
// If the config file specifies that the application should be
// run in a dev environment, then the development environment
// methods for retrieving studies are added and made available also.
func NewConfig(path string) (*Config, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.NewDecoder(bytes.NewBuffer(b)).Decode(&config)
	if err != nil {
		return nil, err
	}

	// Doing this allows the main method to access these otherwise
	// private methods that should only be used in a dev setting.
	if config.Options.DevEnvironment {
		config.DevEnvironment.DevGetMessages = getMessagesDev
		config.DevEnvironment.DevGetRecentMessages = getRecentMessagesDev
		config.DevEnvironment.DevGetConversations = getConversationsDev
	}

	return &config, nil
}
