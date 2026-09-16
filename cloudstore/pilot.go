package cloudstore

import (
	"os"

	"github.com/bomkz/cloudserver/datastore/jsonstore"
)

func updatePilot(token string, pilotData []byte, name string) error {
	jsonstore.New("./" + token + "/" + name)

	jsonstore.Write(pilotData, "./"+token+"/"+name+"/pilot.json")

	return nil
}

func ensurePilotExists(name, uid string) error {
	if _, err := os.Stat("./" + uid + "/ " + name); os.IsNotExist(err) {
		os.Mkdir("./"+uid+"/"+name, 0755)
	}
	return nil
}
