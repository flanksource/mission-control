package senders

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"context"
	"github.com/flanksource/duty/models"
)

type Pushbullet struct{}

func (p *Pushbullet) Send(ctx context.Context, conn *models.Connection, data Data) error {
	token := conn.Password
	if token == "" {
		return fmt.Errorf("pushbullet connection requires a token (password)")
	}

	payload := map[string]string{
		"type":  "note",
		"title": data.Title,
		"body":  data.Message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.pushbullet.com/v2/pushes", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Access-Token", token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pushbullet API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
