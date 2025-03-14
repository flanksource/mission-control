package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/tmc/langchaingo/llms"
	"google.golang.org/genai"
)

var DiagnosisSchema = genai.Schema{
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
}

var PlaybookRecommendationSchema = genai.Schema{
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
}

// GeminiModelWrapper is a wrapper around the Gemini SDK that implements the langchaingo Model interface
type GeminiModelWrapper struct {
	model          string
	client         *genai.Client
	ResponseFormat ResponseFormat
}

// Call implements the langchaingo Model interface for GeminiModelWrapper
func (g *GeminiModelWrapper) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	resp, err := g.GenerateContent(ctx, []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, prompt)}, options...)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("no response from Gemini")
	}
	return resp.Choices[0].Content, nil
}

// GenerateContent implements the langchaingo Model interface for GeminiModelWrapper
func (g *GeminiModelWrapper) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	opts := &llms.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	// Convert messages to Gemini format
	var contents []*genai.Content
	for _, msg := range messages {
		parts := make([]*genai.Part, 0, len(msg.Parts))

		for _, part := range msg.Parts {
			switch typedPart := part.(type) {
			case llms.TextContent:
				parts = append(parts, &genai.Part{Text: typedPart.Text})
			default:
				return nil, fmt.Errorf("unsupported content type: %T", part)
			}
		}

		if len(parts) > 0 {
			content := genai.Content{
				Parts: parts,
			}

			switch msg.Role {
			case llms.ChatMessageTypeAI:
				content.Role = "model"
			default:
				content.Role = "user" // gemini only supports user & model roles
			}

			contents = append(contents, &content)
		}
	}

	// Set temperature if provided
	var genOptions genai.GenerateContentConfig
	if opts.Temperature > 0 {
		genOptions.Temperature = lo.ToPtr(float32(opts.Temperature))
	}

	if g.ResponseFormat == ResponseFormatDiagnosis || g.ResponseFormat == ResponseFormatPlaybookRecommendations {
		if g.ResponseFormat == ResponseFormatDiagnosis {
			genOptions.ResponseSchema = &DiagnosisSchema
			genOptions.ResponseMIMEType = "application/json"
		} else if g.ResponseFormat == ResponseFormatPlaybookRecommendations {
			genOptions.ResponseSchema = &PlaybookRecommendationSchema
			genOptions.ResponseMIMEType = "application/json"
		}
	}

	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, &genOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	return convertGeminiResponse(resp)
}

// convertGeminiResponse converts a Gemini response to a langchaingo response
func convertGeminiResponse(resp *genai.GenerateContentResponse) (*llms.ContentResponse, error) {
	if resp == nil || len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	response := &llms.ContentResponse{
		Choices: make([]*llms.ContentChoice, 0, len(resp.Candidates)),
	}

	for _, candidate := range resp.Candidates {
		choice := &llms.ContentChoice{}

		if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					choice.Content = part.Text
					break
				}
			}
		}

		if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
			for _, part := range candidate.Content.Parts {
				if part.FunctionCall != nil {
					argsJSON, err := json.Marshal(part.FunctionCall.Args)
					if err != nil {
						continue
					}

					toolCall := llms.ToolCall{
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: string(argsJSON),
						},
					}
					choice.ToolCalls = append(choice.ToolCalls, toolCall)
				}
			}
		}

		response.Choices = append(response.Choices, choice)
	}

	return response, nil
}
