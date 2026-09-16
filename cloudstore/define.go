package cloudstore

import "github.com/bomkz/cloudsync/pilots"

type updatePilotRequestStruct struct {
	Request string                       `json:"request"`
	Body    updatePilotRequestBodyStruct `json:"body"`
}
type updatePilotRequestBodyStruct struct {
	Token     string            `json:"token"`
	Name      string            `json:"name"`
	PilotData pilots.PilotsFile `json:"pilotData"`
}

type ResponseStruct struct {
	Request string             `json:"request"`
	Body    ResponseBodyStruct `json:"body"`
}

type ResponseBodyStruct struct {
	TypeResp string `json:"type"`
	Content  string `json:"content"`
}
