package actions

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/llm"
	"github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/shorturl"
)

const (
	maxHeaderTextLength   = 150 // Slack doesn't support more than 150 characters in a header
	maxMarkdownTextLength = 3000
	maxSlackURLLength     = 3000 // Maximum URL length before shortening
	maxButtonTextLength   = 75   // Slack button text must be <= 75 characters
)

// Constants for Slack block formatting
const (
	slackBlockTypeSection   = "section"
	slackBlockTypeHeader    = "header"
	slackBlockTypeDivider   = "divider"
	slackBlockTypeActions   = "actions"
	slackBlockTypeButton    = "button"
	slackBlockTypePlainText = "plain_text"
	slackBlockTypeMarkdown  = "mrkdwn"
)

// JSON code block format template
const jsonCodeBlockFormat = "```json\n%s\n```"

// createPlaybookRunShortURL shortens a URL if it exceeds the maximum length
func createPlaybookRunShortURL(ctx context.Context, originalURL string) (string, error) {
	maxLength := maxSlackURLLength
	if contextMaxLength := ctx.Properties().Int("slack.max-url-length", 50); contextMaxLength > 0 {
		if contextMaxLength > maxSlackURLLength {
			ctx.Logger.Warnf("slack.max-url-length property (%d) exceeds maximum allowed length (%d), using default", contextMaxLength, maxSlackURLLength)
		} else {
			maxLength = contextMaxLength
		}
	}

	if len(originalURL) <= maxLength {
		return originalURL, nil
	}

	shortAlias, err := shorturl.Create(ctx, originalURL)
	if err != nil {
		return "", fmt.Errorf("failed to create short URL: %w", err)
	}

	shortURL, err := shorturl.FullShortURL(*shortAlias)
	if err != nil {
		return "", fmt.Errorf("failed to create playbook run URL: %w", err)
	}

	return shortURL, nil
}

// createPlaybookButtons creates a Slack actions block with buttons for recommended playbooks.
func createPlaybookButtons(ctx context.Context, recommendations llm.PlaybookRecommendations) (map[string]any, error) {
	if len(recommendations.Playbooks) == 0 {
		return nil, nil
	}

	elements := make([]map[string]any, 0, len(recommendations.Playbooks))
	for _, playbook := range recommendations.Playbooks {
		runURL := fmt.Sprintf("%s/playbooks/runs?playbook=%s&run=true&config_id=%s",
			api.FrontendURL, playbook.ID, playbook.ResourceID)

		for _, p := range playbook.Parameters {
			runURL += fmt.Sprintf("&params.%s=%s", p.Key, url.QueryEscape(p.Value))
		}

		finalURL, err := createPlaybookRunShortURL(ctx, runURL)
		if err != nil {
			return nil, fmt.Errorf("failed to shorten playbook URL: %w", err)
		}

		elements = append(elements, map[string]any{
			"type": slackBlockTypeButton,
			"text": map[string]any{
				"type": slackBlockTypePlainText,
				"text": lo.Ellipsis(fmt.Sprintf("%s %s", playbook.Emoji, playbook.Title), maxButtonTextLength),
			},
			"url": finalURL,
		})
	}

	return map[string]any{
		"type":     slackBlockTypeActions,
		"block_id": "playbook_actions",
		"elements": elements,
	}, nil
}

// createResourceActionButtons creates a Slack actions block with buttons for resource actions.
func createResourceActionButtons(resourceID string) map[string]any {
	viewConfigURL := fmt.Sprintf("%s/catalog/%s", api.FrontendURL, resourceID)
	silenceURL := fmt.Sprintf("%s/notifications/silences/add?config_id=%s", api.FrontendURL, resourceID)

	return map[string]any{
		"type":     slackBlockTypeActions,
		"block_id": "resource_actions",
		"elements": []map[string]any{
			{
				"type":  slackBlockTypeButton,
				"style": "primary",
				"text": map[string]any{
					"type":  slackBlockTypePlainText,
					"text":  "View Config",
					"emoji": true,
				},
				"url": viewConfigURL,
			},
			{
				"type": slackBlockTypeButton,
				"text": map[string]any{
					"type":  slackBlockTypePlainText,
					"text":  "🔕 Silence",
					"emoji": true,
				},
				"url": silenceURL,
			},
		},
	}
}

// slackBlocks generates a Slack message with blocks for the diagnosis report and recommendations.
// It returns the JSON string representation of the Slack blocks.
func slackBlocks(ctx context.Context, knowledge *KnowledgeGraph, diagnosisReport llm.DiagnosisReport, recommendations llm.PlaybookRecommendations, groupedResources []string) (string, error) {
	var blocks []map[string]any
	divider := map[string]any{"type": slackBlockTypeDivider}
	affectedResource := knowledge.Configs[0]

	blocks = append(blocks, map[string]any{
		"type": slackBlockTypeHeader,
		"text": map[string]any{
			"type": slackBlockTypePlainText,
			"text": lo.Ellipsis(diagnosisReport.Headline, maxHeaderTextLength),
		},
	})

	if trimmedTagsAndLabels := affectedResource.GetTrimmedLabels(); len(trimmedTagsAndLabels) > 0 {
		blocks = append(blocks, notification.CreateSlackFieldsSection(trimmedTagsAndLabels))
	}

	blocks = append(blocks, divider)

	blocks = append(blocks, markdownSection(fmt.Sprintf("*Summary:*\n%s", diagnosisReport.Summary)))

	blocks = append(blocks, markdownSection(fmt.Sprintf("*Recommended Fix:*\n%s", diagnosisReport.RecommendedFix)))

	if len(groupedResources) > 0 {
		blocks = append(blocks, markdownSection(fmt.Sprintf("*Also Affected:* \n- %s", strings.Join(groupedResources, "\n - "))))
	}

	if playbookButtons, err := createPlaybookButtons(ctx, recommendations); err != nil {
		return "", fmt.Errorf("failed to create playbook buttons: %w", err)
	} else if playbookButtons != nil {
		blocks = append(blocks, playbookButtons)
	}

	resourceButtons := createResourceActionButtons(affectedResource.ID)
	blocks = append(blocks, resourceButtons)

	slackBlocks, err := json.Marshal(map[string]any{
		"blocks": blocks,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal blocks: %w", err)
	}

	return string(slackBlocks), nil
}

func markdownSection(text string) map[string]any {
	return map[string]any{
		"type": slackBlockTypeSection,
		"text": map[string]any{
			"type": slackBlockTypeMarkdown,
			"text": lo.Ellipsis(text, maxMarkdownTextLength),
		},
	}
}
