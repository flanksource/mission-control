package tools

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/shutdown"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

const ToolExtractDiagnosis = "extract_diagnosis"

// NOTE: OpenAI response format doesn't support max length.
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
			Description: "Brief slack flavored markdown summary (≤50 words) of the issue and impact. Slack uses single * for bold, _ for italic and asterisk<space> for bullet point.",
		},
		"recommended_fix": {
			Type:        "string",
			Description: "Slack flavored markdown bullet array of 1–5 concise fixes (≤10 words each). Slack uses single * for bold, _ for italic and asterisk<space> for bullet point.",
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
