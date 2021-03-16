package pecan

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
)

// Config contains any and all configuration items
// for the proper functioning of this application.
type Config struct {
	API struct {
		Use   string `json:"use"`
		Slack struct {
			Token             string `json:"token"`
			VerificationToken string `json:"verification_token"`
			ClientId          string `json:"client_id"`
			ClientSecret      string `json:"client_secret"`
		} `json:"slack"`
	}
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

	return &config, nil
}
