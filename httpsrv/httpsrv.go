package httpsrv

import (
	"bytes"
	"io"

	"github.com/gin-gonic/gin"
)

func Start() {

	r := gin.Default()
	r.POST("/v1", handleRequest)
	r.Run(":8080")
}

func handleRequest(c *gin.Context) {

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(400)
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var req request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatus(400)
		return
	}
	for _, handler := range Handlers {
		if handler.Req == req.Req {
			handler.Handler(body, c)
			return
		}
	}
	c.AbortWithStatus(404)
}
