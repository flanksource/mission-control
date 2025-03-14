package tools

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/shutdown"
	"github.com/google/generative-ai-go/genai"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

const ToolPlaybookRecommendations = "recommend_playbook"

var RecommendPlaybooksToolSchema = openai.ResponseFormatJSONSchemaProperty{
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
						Type:        "array",
						Description: "A list of parameters to pass to the playbook.",
						Items: &openai.ResponseFormatJSONSchemaProperty{
							Type: "object",
							Properties: map[string]*openai.ResponseFormatJSONSchemaProperty{
								"key":   {Type: "string", Description: "The key of the parameter"},
								"value": {Type: "string", Description: "The value of the parameter"},
							},
							Required: []string{"key", "value"},
						},
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

var RecommendPlaybook = llms.Tool{
	Type: "function",
	Function: &llms.FunctionDefinition{
		Name:        ToolPlaybookRecommendations,
		Strict:      true,
		Description: "Extract playbook recommendations from the input",
	},
}

var GeminiRecommendPlaybookTool = &genai.Tool{
	FunctionDeclarations: []*genai.FunctionDeclaration{
		{
			Name:        ToolPlaybookRecommendations,
			Description: "Extract playbook recommendations from the input",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"playbooks": {
						Type:        genai.TypeArray,
						Description: "List of recommended playbooks to fix the issue. The playbooks are sorted by relevance to the issue. Only include playbooks that are relevant to the issue. It's okay if the list is empty.",
						Items: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"id": {
									Type:        genai.TypeString,
									Description: "The UUID of the playbook",
								},
								"emoji": {
									Type:        genai.TypeString,
									Description: "The emoji to represent the playbook",
								},
								"title": {
									Type:        genai.TypeString,
									Description: "The title of the playbook",
								},
								"parameters": {
									Type:        genai.TypeArray,
									Description: "A list of parameters to pass to the playbook.",
									Items: &genai.Schema{
										Type: genai.TypeObject,
										Properties: map[string]*genai.Schema{
											"key":   {Type: genai.TypeString, Description: "The key of the parameter"},
											"value": {Type: genai.TypeString, Description: "The value of the parameter"},
										},
										Required: []string{"key", "value"},
									},
								},
								"resource_id": {
									Type:        genai.TypeString,
									Description: "The UUID of the resource on which the playbook should operate.",
								},
							},
							Required: []string{"id", "emoji", "title", "parameters", "resource_id"},
						},
					},
				},
				Required: []string{"playbooks"},
			},
		},
	},
}

func init() {
	if m, err := json.Marshal(RecommendPlaybooksToolSchema); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to marshal recommendationToolSchema: %v", err))
	} else if err := json.Unmarshal(m, &RecommendPlaybook.Function.Parameters); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to unmarshal recommendationToolSchema: %v", err))
	}
}
