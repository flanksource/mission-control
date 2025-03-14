package tools

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/shutdown"
	"github.com/google/generative-ai-go/genai"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

const ToolExtractDiagnosis = "extract_diagnosis"

var ExtractDiagnosisToolSchema = openai.ResponseFormatJSONSchemaProperty{
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

var ExtractDiagnosis = llms.Tool{
	Type: "function",
	Function: &llms.FunctionDefinition{
		Name:        ToolExtractDiagnosis,
		Description: "Extract diagnosis information from the input",
		Strict:      true,
	},
}

var GeminiDiagnosisTool = &genai.Tool{
	FunctionDeclarations: []*genai.FunctionDeclaration{
		{
			Name:        ToolExtractDiagnosis,
			Description: "Extract diagnosis information from the input",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"headline": {
						Type:        genai.TypeString,
						Description: "Headline that clearly mentions the affected resource & the issue. Feel free to add emojis. Keep it short and concise.",
					},
					"summary": {
						Type:        genai.TypeString,
						Description: "Summary of the issue in markdown. Use bullet points if needed.",
					},
					"recommended_fix": {
						Type:        genai.TypeString,
						Description: "Short and concise recommended fix for the issue in markdown. Use bullet points if needed.",
					},
				},
				Required: []string{"headline", "summary", "recommended_fix"},
			},
		},
	},
}

func init() {
	if m, err := json.Marshal(ExtractDiagnosisToolSchema); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to marshal diagnosisToolSchema: %v", err))
	} else if err := json.Unmarshal(m, &ExtractDiagnosis.Function.Parameters); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to unmarshal diagnosisToolSchema: %v", err))
	}
}
