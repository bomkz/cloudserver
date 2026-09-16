package cloudstore

import (
	"os"

	"github.com/bomkz/cloudserver/auth"
)

func ensureUserExists(token string) error {
	uid, err := auth.CheckAuth(token)
	if err != nil {
		return err
	}
	err = checkUserExists(uid)
	if err != nil {
		return err
	}
	return nil
}

func checkUserExists(uid string) error {
	_, err := os.Stat("./" + uid)

	if os.IsNotExist(err) {

	}
	err = createUser(uid)
	if err != nil {
		return err
	}
	return nil
}

func createUser(uid string) error {
	err := os.Mkdir("./"+uid, 0755)
	if err != nil {
		return err
	}
	return nil
}
