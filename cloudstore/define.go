package cloudstore

type ResponseStruct struct {
	Request string             `json:"request"`
	Body    ResponseBodyStruct `json:"body"`
}

type ResponseBodyStruct struct {
	TypeResp string `json:"type"`
	Content  string `json:"content"`
}
