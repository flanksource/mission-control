package components

import (
	gocontext "context"
	"encoding/json"
	"io"
	"net/url"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
)

func GetLogsByComponent(ctx context.Context, componentID, start, end string) (api.ComponentLogs, error) {
	var logs api.LogsResponse
	var row struct {
		Name       string
		ExternalID string
		Type       string
	}
	err := ctx.DB().Table("components").Select("name", "external_id", "type").Where("id = ?", componentID).Find(&row).Error
	if err != nil {
		return api.ComponentLogs{}, err
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
		Start: start,
		End:   end,
	}
	payloadBytes, err := json.Marshal(&payload)
	if err != nil {
		return api.ComponentLogs{}, err
	}

	endpoint, err := url.JoinPath(api.ApmHubPath, "/search")
	if err != nil {
		return api.ComponentLogs{}, err
	}

	resp, err := http.NewClient().R(gocontext.Background()).Header("Content-Type", "application/json").Post(endpoint, payloadBytes)
	if err != nil {
		return api.ComponentLogs{}, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.ComponentLogs{}, err
	}

	err = json.Unmarshal(body, &logs)
	if err != nil {
		return api.ComponentLogs{}, err
	}

	componentLogs := api.ComponentLogs{
		ID:   componentID,
		Name: row.Name,
		Type: row.Type,
		Logs: logs.Results,
	}

	return componentLogs, nil
}
