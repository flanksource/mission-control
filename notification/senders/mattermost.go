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

type Mattermost struct{}

func (m *Mattermost) Send(ctx context.Context, conn *models.Connection, data Data) error {
	webhookURL := conn.URL
	if webhookURL == "" {
		if props := conn.Properties; props != nil {
			webhookURL = props["webhookURL"]
		}
	}
	if webhookURL == "" {
		return fmt.Errorf("mattermost connection requires a webhook URL")
	}

	payload := mattermostPayload{
		Text:     data.Message,
		Username: conn.Username,
	}
	if data.Title != "" {
		payload.Text = fmt.Sprintf("### %s\n\n%s", data.Title, data.Message)
	}
	if props := conn.Properties; props != nil {
		if ch := props["channel"]; ch != "" {
			payload.Channel = ch
		}
		if icon := props["iconURL"]; icon != "" {
			payload.IconURL = icon
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := httpClient.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mattermost webhook returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

type mattermostPayload struct {
	Channel  string `json:"channel,omitempty"`
	Username string `json:"username,omitempty"`
	IconURL  string `json:"icon_url,omitempty"`
	Text     string `json:"text"`
}
