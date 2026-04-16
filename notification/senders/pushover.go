package senders

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"context"
	"github.com/flanksource/duty/models"
)

type Pushover struct{}

func (p *Pushover) Send(ctx context.Context, conn *models.Connection, data Data) error {
	token := conn.Password
	user := conn.Username
	if token == "" || user == "" {
		return fmt.Errorf("pushover connection requires token (password) and user (username)")
	}

	form := url.Values{
		"token":   {token},
		"user":    {user},
		"title":   {data.Title},
		"message": {data.Message},
		"html":    {"1"},
	}

	resp, err := httpClient.PostForm("https://api.pushover.net/1/messages.json", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pushover API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
