package actions

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
	pkgArtifacts "github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/notification"
)

type Notification struct {
	RunID uuid.UUID
}

type NotificationResult struct {
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
	Slack   string `json:"slack,omitempty"`
}

func (t *Notification) Run(ctx context.Context, action v1.NotificationAction) (*NotificationResult, error) {
	notifContext := notification.NewContext(ctx, uuid.Nil)
	var attachments []notification.Attachment
	for _, a := range action.Attachments {
		if strings.HasPrefix(a.Content, "artifact://") {
			resolved, err := resolveArtifactAttachments(ctx, t.RunID, a)
			if err != nil {
				return nil, ctx.Oops().Wrapf(err, "failed to resolve artifact attachment")
			}
			attachments = append(attachments, resolved...)
			continue
		}

		// Attempt to decode base64 content (e.g. binary formats from ReportAction)
		// to avoid double-encoding when the MIME library encodes for email transport.
		content := []byte(a.Content)
		if decoded, err := base64.StdEncoding.DecodeString(a.Content); err == nil {
			content = decoded
		}

		attachments = append(attachments, notification.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Content:     content,
		})
	}

	data := notification.NotificationTemplate{
		Title:       action.Title,
		Message:     action.Message,
		Properties:  action.Properties,
		Attachments: attachments,
	}

	service, err := notification.SendRawNotification(notifContext, action.Connection, action.URL, nil, data, nil)
	if err != nil {
		return nil, err
	}

	output := &NotificationResult{
		Title:   data.Title,
		Message: data.Message,
	}
	if service == "slack" {
		if notification.IsSlackBlocksJSON(data.Message) {
			output.Slack = data.Message
		} else {
			payload := notification.NotificationMessagePayload{
				Title:       data.Title,
				Description: data.Message,
			}
			if slackMsg, err := notification.FormatNotificationMessage(payload, "slack"); err == nil {
				output.Slack = slackMsg
			}
		}
	}

	return output, nil
}

// resolveArtifactAttachments resolves an artifact:// URI into notification attachments
// by loading the referenced playbook action's stored artifacts and matching them
// against an optional glob pattern (e.g. "artifact://generate-pdf/*.pdf").
func resolveArtifactAttachments(ctx context.Context, runID uuid.UUID, attachment v1.NotificationAttachment) ([]notification.Attachment, error) {
	uri := strings.TrimPrefix(attachment.Content, "artifact://")
	actionName, glob, _ := strings.Cut(uri, "/")

	var action models.PlaybookRunAction
	if err := ctx.DB().Where("name = ? AND playbook_run_id = ?", actionName, runID).
		Order("start_time ASC").Limit(1).First(&action).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "action %q not found in run %s", actionName, runID)
	}

	var dbArtifacts []models.Artifact
	if err := ctx.DB().Where("playbook_run_action_id = ?", action.ID).Order("created_at, id").Find(&dbArtifacts).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to get artifacts for action %s", action.ID)
	}

	contents, err := pkgArtifacts.GetArtifactContents(ctx, action.ID.String())
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to load artifact contents")
	}

	// Build a map from action ID to content for safe lookup instead of relying on index ordering.
	contentByPath := make(map[string][]byte, len(contents))
	for _, c := range contents {
		contentByPath[c.Path] = c.Content
	}

	var result []notification.Attachment
	for _, a := range dbArtifacts {
		if glob != "" {
			matched, err := filepath.Match(glob, a.Filename)
			if err != nil {
				return nil, ctx.Oops().Wrapf(err, "invalid glob pattern %q", glob)
			}
			if !matched {
				continue
			}
		}
		content, ok := contentByPath[a.Path]
		if !ok {
			continue
		}
		filename := attachment.Filename
		if filename == "" {
			filename = a.Filename
		}
		contentType := attachment.ContentType
		if contentType == "" {
			contentType = a.ContentType
		}
		result = append(result, notification.Attachment{
			Filename:    filename,
			ContentType: contentType,
			Content:     content,
		})
	}

	return result, nil
}
