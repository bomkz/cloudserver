package main

import (
	"github.com/bomkz/cloudserver/cloudstore"
	"github.com/bomkz/cloudserver/httpsrv"
)

func main() {
	go httpsrv.Start()
	go cloudstore.InitCloudserver()
	<-Stop
}

var Stop = make(chan bool)
