package cloudstore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bomkz/cloudserver/auth"
	"github.com/bomkz/cloudserver/httpsrv"
	"github.com/bomkz/cloudserver/jsonstore"
	"github.com/bomkz/cloudsync/pilots"
	"github.com/gin-gonic/gin"
)

func InitCloudserver() {
	httpsrv.Handlers = append(httpsrv.Handlers, httpsrv.ReqHandlers{
		Req:     "updatePilot",
		Handler: updatePilotHandler,
	})
}

func updatePilotHandler(requestByte []byte, c *gin.Context) {
	request := pilots.UpdatePilotRequestStruct{}

	err := json.Unmarshal(requestByte, &request)
	if err != nil {
		resp := ResponseStruct{}

		resp.Request = "response"
		resp.Body.TypeResp = "error"
		resp.Body.Content = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(400, resp)
		return
	}

	uuid, err := auth.CheckAuth(request.Body.Token)
	if err != nil {
		resp := ResponseStruct{}

		resp.Request = "response"
		resp.Body.TypeResp = "error"
		resp.Body.Content = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(401, resp)
		return
	}

	pilotByte, err := json.Marshal(request.Body.PilotData)
	if err != nil {
		resp := ResponseStruct{}

		resp.Request = "response"
		resp.Body.TypeResp = "error"
		resp.Body.Content = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(422, resp)
		return
	}

	err = updatePilot(uuid, pilotByte, request.Body.Name)
	if err != nil {
		resp := ResponseStruct{}

		resp.Request = "response"
		resp.Body.TypeResp = "error"
		resp.Body.Content = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	} else {
		resp := ResponseStruct{}

		resp.Request = "response"
		resp.Body.TypeResp = "success"
		resp.Body.Content = fmt.Sprint(time.Now().Unix())
		c.JSON(200, resp)
	}
}

func updatePilot(uuid string, pilotData []byte, name string) error {
	pilotDir, err := jsonstore.New("./data/" + uuid + "/" + name)

	if err != nil {
		return err
	}

	pilot := pilots.PilotsFile{}
	err = json.Unmarshal(pilotData, &pilot)
	if err != nil {
		return err
	}

	_, err = pilotDir.SaveWithMessage("pilot.json", pilot, time.Now().String())
	if err != nil {
		return err
	}

	return nil
}
