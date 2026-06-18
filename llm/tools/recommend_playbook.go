package tools

const ToolPlaybookRecommendations = "recommend_playbook"

var RecommendPlaybooksToolSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]any{
		"playbooks": map[string]any{
			"type":        "array",
			"description": "List of recommended playbooks to fix the issue. The playbooks are sorted by relevance to the issue. Only include playbooks that are relevant to the issue. It's okay if the list is empty.",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "The UUID of the playbook",
					},
					"emoji": map[string]any{
						"type":        "string",
						"description": "The emoji to represent the playbook",
					},
					"title": map[string]any{
						"type":        "string",
						"description": "The title of the playbook",
					},
					"parameters": map[string]any{
						"type":        "array",
						"description": "A list of parameters to pass to the playbook.",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]any{
								"key": map[string]any{
									"type":        "string",
									"description": "The key of the parameter",
								},
								"value": map[string]any{
									"type":        "string",
									"description": "The value of the parameter",
								},
							},
							"required": []string{"key", "value"},
						},
					},
					"resource_id": map[string]any{
						"type":        "string",
						"description": "The UUID of the resource on which the playbook should operate.",
					},
				},
				"required": []string{"id", "emoji", "title", "parameters", "resource_id"},
			},
		},
	},
	"required": []string{"playbooks"},
}
