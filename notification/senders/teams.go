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

type Teams struct{}

func (t *Teams) Send(ctx context.Context, conn *models.Connection, data Data) error {
	webhookURL := conn.URL
	if webhookURL == "" {
		if props := conn.Properties; props != nil {
			webhookURL = props["webhookURL"]
		}
	}
	if webhookURL == "" {
		return fmt.Errorf("teams connection requires a webhook URL")
	}

	card := teamsMessageCard{
		Type:       "MessageCard",
		Context:    "http://schema.org/extensions",
		ThemeColor: "0076D7",
		Summary:    data.Title,
		Sections: []teamsSection{{
			ActivityTitle: data.Title,
			Text:          data.Message,
			Markdown:      true,
		}},
	}

	body, err := json.Marshal(card)
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
		return fmt.Errorf("teams webhook returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

type teamsMessageCard struct {
	Type       string         `json:"@type"`
	Context    string         `json:"@context"`
	ThemeColor string         `json:"themeColor,omitempty"`
	Summary    string         `json:"summary"`
	Sections   []teamsSection `json:"sections"`
}

type teamsSection struct {
	ActivityTitle string `json:"activityTitle,omitempty"`
	Text          string `json:"text"`
	Markdown      bool   `json:"markdown"`
}
