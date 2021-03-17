package addon

import (
	"github.com/gin-gonic/gin"
	"github.com/ielab/pecan"
	"github.com/olivere/elastic/v7"
)

type Addon interface {
	Initialise(es *elastic.Client, api pecan.ChatAPI, config *pecan.Config)
	Handler() gin.HandlerFunc
}

var Addons = map[string]Addon{
	"evaluation": NewEvaluationAddon(),
}
