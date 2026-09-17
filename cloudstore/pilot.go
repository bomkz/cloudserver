package cloudstore

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bomkz/cloudserver/httpsrv"
	"github.com/bomkz/cloudserver/jsonstore"
	"github.com/bomkz/cloudserver/srvauth"
	"github.com/bomkz/cloudsync/global"
	"github.com/gin-gonic/gin"
)

func InitCloudserver() {
	httpsrv.Handlers = append(httpsrv.Handlers, httpsrv.ReqHandlers{
		Req:     "updatePilot",
		Handler: updatePilotHandler,
	})
	httpsrv.Handlers = append(httpsrv.Handlers, httpsrv.ReqHandlers{
		Req:     "getPilots",
		Handler: getPilotsHandler,
	})
	httpsrv.Handlers = append(httpsrv.Handlers, httpsrv.ReqHandlers{
		Req:     "getPilotInfo",
		Handler: getPilotInfoHandler,
	})
	httpsrv.Handlers = append(httpsrv.Handlers, httpsrv.ReqHandlers{
		Req:     "getPilot",
		Handler: getPilotHandler,
	})
}

func getPilotHandler(requestByte []byte, c *gin.Context) {

	request := global.GetPilotReqStruct{}
	err := json.Unmarshal(requestByte, &request)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(400, resp)
		return
	}

	uuid, err := srvauth.CheckAuth(request.Auth)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(401, resp)
		return
	}

	jstore, err := jsonstore.New(fmt.Sprintf(userStore, uuid))
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	pilot, err := jstore.LoadVersion(pilotPrefix+request.Pilot.Name, request.Pilot.Version)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	pilotResp := global.GetPilotRespStruct{}
	pilotResp.Req = "success"
	err = json.Unmarshal(pilot, &pilotResp.Pilot)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	c.JSON(200, pilotResp)
}

func updatePilotHandler(requestByte []byte, c *gin.Context) {
	request := global.UpdatePilotRequestStruct{}

	err := json.Unmarshal(requestByte, &request)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(400, resp)
		return
	}

	uuid, err := srvauth.CheckAuth(request.Auth)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(401, resp)
		return
	}

	pilotByte, err := json.Marshal(request.Body.PilotData)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(422, resp)
		return
	}

	err = updatePilot(uuid, pilotByte, request.Body.Name)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"
		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	} else {
		resp := global.ResponseStruct{}

		resp.Request = "success"
		resp.Body = fmt.Sprint(time.Now().Unix())
		c.JSON(200, resp)
	}
}

func updatePilot(uuid string, pilotData []byte, name string) error {
	userDir, err := jsonstore.New(fmt.Sprintf(userStore, uuid))

	if err != nil {
		return err
	}

	pilot := global.PilotsFile{}
	err = json.Unmarshal(pilotData, &pilot)
	if err != nil {
		return err
	}

	_, err = userDir.SaveWithMessage(pilotPrefix+name, pilot, fmt.Sprint(time.Now().Unix()))
	if err != nil {
		return err
	}

	return nil
}

func getPilotInfoHandler(requestByte []byte, c *gin.Context) {

	request := global.GetPilotInfoReqStruct{}

	err := json.Unmarshal(requestByte, &request)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(400, resp)
		return
	}
	uuid, err := srvauth.CheckAuth(request.Auth)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(401, resp)
		return
	}
	_, err = os.Stat(fmt.Sprintf(userStore, uuid))
	if err != nil && os.IsNotExist(err) {

		response := global.GetPilotInfoRespStruct{}
		response.Req = "success"
		c.JSON(200, response)
		return
	} else if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	pilotInfo, err := jsonstore.New(fmt.Sprintf(userStore, uuid))
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	versions, err := pilotInfo.History(pilotPrefix + request.Name)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	var versionResp global.GetPilotInfoRespStruct
	versionResp.Req = "success"
	versionResp.Content = len(versions) + 1

	c.JSON(200, versionResp)
}

func getPilotsHandler(requestByte []byte, c *gin.Context) {

	var request = global.GetPilotsReqStruct{}

	err := json.Unmarshal(requestByte, &request)

	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(400, resp)
		return
	}

	uuid, err := srvauth.CheckAuth(request.Auth)
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(401, resp)
		return
	}

	_, err = os.Stat(fmt.Sprintf(userStore, uuid))
	if err != nil && os.IsNotExist(err) {

		response := global.GetPilotsResponseStruct{}
		response.Request = "success"
		response.Body = []string{}
		c.JSON(200, response)
		return
	} else if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	pilotsData, err := jsonstore.New(fmt.Sprintf(userStore, uuid))
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}
	keys, err := pilotsData.Keys()
	if err != nil {
		resp := global.ResponseStruct{}

		resp.Request = "error"

		resp.Body = fmt.Sprint(time.Now().Unix()) + err.Error()
		c.JSON(500, resp)
		return
	}

	pilotNames := []string{}
	for _, key := range keys {
		if strings.HasPrefix(key, pilotPrefix) {
			pilotNames = append(pilotNames, strings.TrimPrefix(key, pilotPrefix))
		}
	}

	response := global.GetPilotsResponseStruct{}
	response.Request = "success"
	response.Body = pilotNames
	c.JSON(200, response)
}
