package slackarchive

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
)

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
}

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
