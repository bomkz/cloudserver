package httpsrv

import "github.com/gin-gonic/gin"

type request struct {
	Req string `json:"req"`
}

type ReqHandlers struct {
	Req     string
	Handler func([]byte, *gin.Context)
}

var Handlers []ReqHandlers
