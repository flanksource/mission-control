package tools

const ToolExtractDiagnosis = "extract_diagnosis"

var ExtractDiagnosisToolSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]any{
		"headline": map[string]any{
			"type":        "string",
			"description": "Headline that clearly mentions the affected resource & the issue. Feel free to add emojis. Keep it short and concise.",
		},
		"summary": map[string]any{
			"type":        "string",
			"description": "Brief slack flavored markdown summary (≤50 words) of the issue and impact. Slack uses single * for bold, _ for italic and asterisk<space> for bullet point.",
		},
		"recommended_fix": map[string]any{
			"type":        "string",
			"description": "Slack flavored markdown bullet array of 1–5 concise fixes (≤10 words each). Slack uses single * for bold, _ for italic and asterisk<space> for bullet point.",
		},
	},
	"required": []string{"headline", "summary", "recommended_fix"},
}
