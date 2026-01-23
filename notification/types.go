package notification

import (
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
