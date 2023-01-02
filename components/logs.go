package components

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"

	"github.com/flanksource/commons/http"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

func GetLogsByComponent(componentID, logStart, logEnd string) (api.LogsResponse, error) {
	var logs api.LogsResponse
	var row struct {
		ExternalID string
		Type       string
	}
	err := db.Gorm.Table("components").Select("external_id", "type").Where("id = ?", componentID).Find(&row).Error
	if err != nil {
		return logs, err
	}

	type payloadBody struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Start string `json:"start"`
		End   string `json:"end"`
	}

	payload := payloadBody{
		ID:    row.ExternalID,
		Type:  row.Type,
		Start: logStart,
		End:   logEnd,
	}
	payloadBytes, err := json.Marshal(&payload)
	if err != nil {
		return logs, err
	}

	endpoint, err := url.JoinPath(api.ApmHubPath, "/search")
	if err != nil {
		return logs, err
	}

	client := http.NewClient(&http.Config{})
	resp, err := client.Post(endpoint, "application/json", io.NopCloser(strings.NewReader(string(payloadBytes))))
	if err != nil {
		return logs, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return logs, err
	}

	err = json.Unmarshal(body, &logs)
	if err != nil {
		return logs, err
	}

	return logs, nil
}
