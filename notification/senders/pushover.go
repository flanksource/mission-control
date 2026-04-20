package senders

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.pushover.net/1/messages.json", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
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
