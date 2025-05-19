package tools

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/shutdown"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

const ToolExtractDiagnosis = "extract_diagnosis"

// NOTE: OpenAI response format doen't support max length.
// Notable keywords not supported include:
// For strings: minLength, maxLength, pattern, format
// https://platform.openai.com/docs/guides/structured-outputs?api-mode=responses#some-type-specific-keywords-are-not-yet-supported

var ExtractDiagnosisToolSchema = openai.ResponseFormatJSONSchemaProperty{
	Type: "object",
	Properties: map[string]*openai.ResponseFormatJSONSchemaProperty{
		"headline": {
			Type:        "string",
			Description: "Headline that clearly mentions the affected resource & the issue. Feel free to add emojis. Keep it short and concise.",
		},
		"summary": {
			Type:        "string",
			Description: "Summary of the issue in markdown. Keep it short and concise under 50 words.",
		},
		"recommended_fix": {
			Type:        "string",
			Description: "Short and concise recommended fix for the issue in markdown. Use bullet points if needed. Keep each points under 10 words.",
		},
	},
	Required: []string{"headline", "summary", "recommended_fix"},
}

var ExtractDiagnosis = llms.Tool{
	Type: "function",
	Function: &llms.FunctionDefinition{
		Name:        ToolExtractDiagnosis,
		Description: "Extract diagnosis information from the input",
		Strict:      true,
	},
}

func init() {
	// use the json schema defined for OpenAI in llm tools
	if m, err := json.Marshal(ExtractDiagnosisToolSchema); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to marshal diagnosisToolSchema: %v", err))
	} else if err := json.Unmarshal(m, &ExtractDiagnosis.Function.Parameters); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to unmarshal diagnosisToolSchema: %v", err))
	}
}
