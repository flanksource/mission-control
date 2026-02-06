package notification

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
)

// NotificationSendHistoryDetail represents notification history with rendered body details.
type NotificationSendHistoryDetail struct {
	models.NotificationSendHistory `json:",inline"`
	ResourceTags                   types.JSONStringMap `json:"resource_tags,omitempty"`
	Resource                       types.JSON          `json:"resource,omitempty"`
	ResourceKind                   string              `json:"resource_kind,omitempty"`
	ResourceType                   *string             `json:"resource_type,omitempty"`
	PlaybookRun                    types.JSON          `json:"playbook_run,omitempty"`
	BodyMarkdown                   string              `json:"body_markdown,omitempty"`
}

// RenderBodyMarkdown returns a markdown representation of the notification body.
// It prefers body_payload (rendered via clicky) and falls back to the legacy body column.
func RenderBodyMarkdown(h models.NotificationSendHistory) string {
	if len(h.BodyPayload) > 0 {
		var payload NotificationMessagePayload
		if err := json.Unmarshal(h.BodyPayload, &payload); err == nil {
			if md, err := FormatNotificationMessage(payload, "markdown"); err == nil {
				return md
			}
		}
	}

	if h.Body != nil {
		return *h.Body
	}

	return ""
}
