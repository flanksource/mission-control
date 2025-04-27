package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ollama/ollama/api"
	"github.com/tmc/langchaingo/llms"
)

type OllamaClient struct {
	*api.Client
	model string
}

func NewOllamaClient(model, baseURL string) (*OllamaClient, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	c := api.NewClient(parsedURL, &http.Client{})
	return &OllamaClient{Client: c, model: model}, nil
}

type Params struct {
	Type       string   `json:"type"`
	Defs       any      `json:"$defs,omitempty"`
	Items      any      `json:"items,omitempty"`
	Required   []string `json:"required"`
	Properties map[string]struct {
		Type        api.PropertyType `json:"type"`
		Items       any              `json:"items,omitempty"`
		Description string           `json:"description"`
		Enum        []any            `json:"enum,omitempty"`
	} `json:"properties"`
}

// GenerateContent asks the model to generate content from a sequence of
// messages. It's the most general interface for multi-modal LLMs that support
// chat-like interactions.
func (c *OllamaClient) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// https://ollama.com/blog/tool-support
	tools := []api.Tool{
		{
			Type: "function",
			Function: api.ToolFunction{
				Name:        "report_diagnosis",
				Description: "Report the diagnosis information for the affected resource",
				Parameters: Params{
					Type: "object",
					Properties: map[string]struct {
						Type        api.PropertyType `json:"type"`
						Items       any              `json:"items,omitempty"`
						Description string           `json:"description"`
						Enum        []any            `json:"enum,omitempty"`
					}{
						"headline": {
							Type:        api.PropertyType([]string{"string"}),
							Description: "Headline that clearly mentions the affected resource & the issue. Feel free to add emojis. Keep it short and concise.",
						},
						"summary": {
							Type:        api.PropertyType([]string{"string"}),
							Description: "Summary of the issue in markdown. Use bullet points if needed.",
						},
						"recommended_fix": {
							Type:        api.PropertyType([]string{"string"}),
							Description: "Short and concise recommended fix for the issue in markdown. Use bullet points if needed.",
						},
					},
					Required: []string{"headline", "summary", "recommended_fix"},
				},
			},
		},
	}

	chatRequest := api.ChatRequest{
		Model: c.model,
		Tools: tools,
	}

	for _, message := range messages {
		for _, part := range message.Parts {
			role := string(message.Role)
			if role == string(llms.ChatMessageTypeHuman) {
				role = "user"
			} else if role == string(llms.ChatMessageTypeAI) {
				role = "assistant"
			}

			chatRequest.Messages = append(chatRequest.Messages, api.Message{
				Role:    role,
				Content: fmt.Sprintf("%s", part),
			})
		}
	}

	var llmResponse strings.Builder
	if err := c.Client.Chat(ctx, &chatRequest, func(resp api.ChatResponse) error {
		_, err := llmResponse.WriteString(resp.Message.Content)
		return err
	}); err != nil {
		return nil, err
	}

	contentResponse := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: llmResponse.String()}},
	}
	return contentResponse, nil
}

// Call is a simplified interface for a text-only Model, generating a single
// string response from a single string prompt.
//
// Deprecated: this method is retained for backwards compatibility. Use the
// more general [GenerateContent] instead. You can also use
// the [GenerateFromSinglePrompt] function which provides a similar capability
// to Call and is built on top of the new interface.
func (c *OllamaClient) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}
