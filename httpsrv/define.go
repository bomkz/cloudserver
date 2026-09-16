package httpsrv

import "github.com/gin-gonic/gin"

type request struct {
	Req string `json:"req"`
}

type reqHandlers struct {
	Req     string
	Handler func([]byte, *gin.Context)
}

var handlers []reqHandlers
