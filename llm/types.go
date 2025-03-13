package llm

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/shutdown"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

type DiagnosisReport struct {
	RecommendedFix string `json:"recommended_fix"`
	Headline       string `json:"headline"`
	Summary        string `json:"summary"`
}

type PlaybookRecommendations struct {
	Playbooks []RecommendedPlaybook `json:"playbooks"`
}

type RecommendedPlaybook struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Emoji      string            `json:"emoji"`
	Parameters map[string]string `json:"parameters"`
	ResourceID string            `json:"resource_id"`
}

var diagnosisToolSchema = openai.ResponseFormatJSONSchemaProperty{
	Type: "object",
	Properties: map[string]*openai.ResponseFormatJSONSchemaProperty{
		"headline": {
			Type:        "string",
			Description: "Headline that clearly mentions the affected resource & the issue. Feel free to add emojis. Keep it short and concise.",
		},
		"summary": {
			Type:        "string",
			Description: "Summary of the issue in markdown. Use bullet points if needed.",
		},
		"recommended_fix": {
			Type:        "string",
			Description: "Short and concise recommended fix for the issue in markdown. Use bullet points if needed.",
		},
	},
	Required: []string{"headline", "summary", "recommended_fix"},
}

var recommendatationToolSchema = openai.ResponseFormatJSONSchemaProperty{
	Type: "object",
	Properties: map[string]*openai.ResponseFormatJSONSchemaProperty{
		"playbooks": {
			Type:        "array",
			Description: "List of recommended playbooks to fix the issue. The playbooks are sorted by relevance to the issue. Only include playbooks that are relevant to the issue. It's okay if the list is empty.",
			Items: &openai.ResponseFormatJSONSchemaProperty{
				Type: "object",
				Properties: map[string]*openai.ResponseFormatJSONSchemaProperty{
					"id": {
						Type:        "string",
						Description: "The UUID of the playbook",
					},
					"emoji": {
						Type:        "string",
						Description: "The emoji to represent the playbook",
					},
					"title": {
						Type:        "string",
						Description: "The title of the playbook",
					},
					"parameters": {
						Type:                 "object",
						Description:          "A key-value (Record<string, string>) pair of parameters to pass to the playbook. Keep in mind the values are all strings even numbers and booleans are strings",
						AdditionalProperties: true,
					},
					"resource_id": {
						Type:        "string",
						Description: "The UUID of the resource on which the playbook should operate.",
					},
				},
				Required: []string{"id", "emoji", "title", "parameters", "resource_id"},
			},
		},
	},
	Required: []string{"playbooks"},
}

var (
	diagnosisTool = llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "extract_diagnosis",
			Description: "Extract diagnosis information from the input",
		},
	}

	playbookRecommendationsTool = llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "extract_playbook_recommendations",
			Description: "Extract playbook recommendations from the input",
		},
	}
)

func init() {
	if m, err := json.Marshal(diagnosisToolSchema); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to marshal diagnosisToolSchema: %v", err))
	} else if err := json.Unmarshal(m, &diagnosisTool.Function.Parameters); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to unmarshal diagnosisToolSchema: %v", err))
	}

	if m, err := json.Marshal(recommendatationToolSchema); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to marshal recommendationToolSchema: %v", err))
	} else if err := json.Unmarshal(m, &playbookRecommendationsTool.Function.Parameters); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to unmarshal recommendationToolSchema: %v", err))
	}
}
