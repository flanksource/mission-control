package tools

import (
	"encoding/json"
	"fmt"

	"github.com/tmc/langchaingo/llms"
)

const ToolCustomSchema = "extract_structured_output"

// CustomSchemaTool creates an LLM tool definition from a raw JSON schema string.
// This is used for Anthropic and other backends that use tool-based schema enforcement.
func CustomSchemaTool(schemaJSON string) (llms.Tool, error) {
	var params map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &params); err != nil {
		return llms.Tool{}, fmt.Errorf("failed to parse custom schema JSON: %w", err)
	}

	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        ToolCustomSchema,
			Description: "Extract structured output matching the provided schema",
			Strict:      true,
			Parameters:  params,
		},
	}, nil
}
