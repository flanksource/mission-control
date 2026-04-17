package senders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/flanksource/duty/models"
)

type Discord struct{}

func (d *Discord) Send(ctx context.Context, conn *models.Connection, data Data) error {
	webhookURL := conn.URL
	if webhookURL == "" {
		if conn.Username == "" || conn.Password == "" {
			return fmt.Errorf("discord connection requires a webhook URL or webhookID (username) and token (password)")
		}
		webhookURL = fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", conn.Username, conn.Password)
	}

	payload := discordWebhookPayload{
		Embeds: []discordEmbed{{
			Title:       data.Title,
			Description: data.Message,
		}},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

type discordWebhookPayload struct {
	Content string         `json:"content,omitempty"`
	Embeds  []discordEmbed `json:"embeds,omitempty"`
}

type discordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color,omitempty"`
}
