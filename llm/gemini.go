package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"github.com/samber/lo"
	"github.com/tmc/langchaingo/llms"
)

// GeminiModelWrapper is a wrapper around the Gemini SDK that implements the langchaingo Model interface
type GeminiModelWrapper struct {
	model *genai.GenerativeModel
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

	if opts.Temperature > 0 {
		g.model.Temperature = lo.ToPtr(float32(opts.Temperature))
	}

	// Convert messages to Gemini format
	var geminiParts []genai.Part
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if textContent, ok := part.(llms.TextContent); ok {
				geminiParts = append(geminiParts, genai.Text(textContent.Text))
			}
		}
	}

	resp, err := g.model.GenerateContent(ctx, geminiParts...)
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
				if textPart, ok := part.(genai.Text); ok {
					choice.Content = string(textPart)
					break
				}
			}
		}

		if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
			for _, part := range candidate.Content.Parts {
				if functionCall, ok := part.(genai.FunctionCall); ok {
					argsJSON, err := json.Marshal(functionCall.Args)
					if err != nil {
						continue
					}

					toolCall := llms.ToolCall{
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      functionCall.Name,
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
