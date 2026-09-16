package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func CheckAuth(auth string) (string, error) {
	user, err := checkUID(auth)
	if err != nil {
		return "", err
	}
	return user.ID, nil
}

func checkUID(auth string) (*DiscordUser, error) {
	req, err := http.NewRequest(http.MethodGet, "https://discord.com/api/users/@me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+auth)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discord returned %s: %s", resp.Status, body)
	}

	var user DiscordUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
}
