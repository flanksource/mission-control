package actions

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/llm"
)

// Constants for Slack block formatting
const (
	maxSlackFieldsPerSection = 10 // Slack doesn't support more than 10 fields in a section
	slackBlockTypeSection    = "section"
	slackBlockTypeHeader     = "header"
	slackBlockTypeDivider    = "divider"
	slackBlockTypeActions    = "actions"
	slackBlockTypeButton     = "button"
	slackBlockTypePlainText  = "plain_text"
	slackBlockTypeMarkdown   = "mrkdwn"
)

// JSON code block format template
const jsonCodeBlockFormat = "```json\n%s\n```"

// createSlackFieldsSection creates a Slack section block with fields from a map of labels.
// It handles sorting and limiting the number of fields to Slack's maximum.
func createSlackFieldsSection(title string, labels map[string]string) map[string]any {
	if len(labels) == 0 {
		return nil
	}

	var fields []map[string]any
	count := 0
	for key, value := range labels {
		if count >= maxSlackFieldsPerSection {
			break
		}

		if strings.TrimSpace(value) == "" {
			continue
		}

		fields = append(fields, map[string]any{
			"type":     slackBlockTypeMarkdown,
			"text":     fmt.Sprintf("*%s*: %s", key, value),
			"verbatim": true,
		})
		count++
	}

	// Sort fields alphabetically
	slices.SortFunc(fields, func(a, b map[string]any) int {
		return strings.Compare(a["text"].(string), b["text"].(string))
	})

	section := map[string]any{
		"type":   slackBlockTypeSection,
		"fields": fields,
	}

	if title != "" {
		section["text"] = map[string]any{
			"type": slackBlockTypeMarkdown,
			"text": fmt.Sprintf("*%s*", title),
		}
	}

	return section
}

// createPlaybookButtons creates a Slack actions block with buttons for recommended playbooks.
func createPlaybookButtons(recommendations llm.PlaybookRecommendations) map[string]any {
	if len(recommendations.Playbooks) == 0 {
		return nil
	}

	elements := make([]map[string]any, 0, len(recommendations.Playbooks))
	for _, playbook := range recommendations.Playbooks {
		runURL := fmt.Sprintf("%s/playbooks/runs?playbook=%s&run=true&config_id=%s",
			api.FrontendURL, playbook.ID, playbook.ResourceID)

		for key, value := range playbook.Parameters {
			runURL += fmt.Sprintf("&params.%s=%s", key, url.QueryEscape(value))
		}

		elements = append(elements, map[string]any{
			"type": slackBlockTypeButton,
			"text": map[string]any{
				"type": slackBlockTypePlainText,
				"text": fmt.Sprintf("%s %s", playbook.Emoji, playbook.Title),
			},
			"url": runURL,
		})
	}

	return map[string]any{
		"type":     slackBlockTypeActions,
		"block_id": "playbook_actions",
		"elements": elements,
	}
}

// createResourceActionButtons creates a Slack actions block with buttons for resource actions.
func createResourceActionButtons(resourceID string) map[string]any {
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
				"url": fmt.Sprintf("%s/catalog/%s", api.FrontendURL, resourceID),
			},
			{
				"type": slackBlockTypeButton,
				"text": map[string]any{
					"type":  slackBlockTypePlainText,
					"text":  "🔕 Silence",
					"emoji": true,
				},
				"url": fmt.Sprintf("%s/notifications/silences/add?config_id=%s", api.FrontendURL, resourceID),
			},
		},
	}
}

// slackBlocks generates a Slack message with blocks for the diagnosis report and recommendations.
// It returns the JSON string representation of the Slack blocks.
func slackBlocks(knowledge *KnowledgeGraph, diagnosisReport llm.DiagnosisReport, recommendations llm.PlaybookRecommendations) (string, error) {
	var blocks []map[string]any
	divider := map[string]any{"type": slackBlockTypeDivider}
	affectedResource := knowledge.Configs[0]

	blocks = append(blocks, map[string]any{
		"type": slackBlockTypeHeader,
		"text": map[string]any{
			"type": slackBlockTypePlainText,
			"text": diagnosisReport.Headline,
		},
	})

	if tagsSection := createSlackFieldsSection("", affectedResource.Tags); tagsSection != nil {
		blocks = append(blocks, tagsSection)
	}
	blocks = append(blocks, divider)

	blocks = append(blocks, map[string]any{
		"type": slackBlockTypeSection,
		"text": map[string]any{
			"type": slackBlockTypeMarkdown,
			"text": fmt.Sprintf("*Summary:*\n%s", diagnosisReport.Summary),
		},
	})

	blocks = append(blocks, map[string]any{
		"type": slackBlockTypeSection,
		"text": map[string]any{
			"type": slackBlockTypeMarkdown,
			"text": fmt.Sprintf("*Recommended Fix:*\n%s", diagnosisReport.RecommendedFix),
		},
	})

	blocks = append(blocks, divider)

	if labelsSection := createSlackFieldsSection("Labels", *affectedResource.Labels); labelsSection != nil {
		blocks = append(blocks, labelsSection)
		blocks = append(blocks, divider)
	}

	if playbookButtons := createPlaybookButtons(recommendations); playbookButtons != nil {
		blocks = append(blocks, playbookButtons)
	}

	blocks = append(blocks, createResourceActionButtons(affectedResource.ID))

	slackBlocks, err := json.Marshal(map[string]any{
		"blocks": blocks,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal blocks: %w", err)
	}

	return string(slackBlocks), nil
}
