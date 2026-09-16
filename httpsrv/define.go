package httpsrv

import "github.com/gin-gonic/gin"

type request struct {
	Req string `json:"request"`
}

type ReqHandlers struct {
	Req     string
	Handler func([]byte, *gin.Context)
}

var Handlers []ReqHandlers

type ResponseStruct struct {
	Request string             `json:"request"`
	Body    ResponseBodyStruct `json:"body"`
}

type ResponseBodyStruct struct {
	TypeResp string `json:"type"`
	Content  string `json:"content"`
}
