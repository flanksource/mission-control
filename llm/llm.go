package llm

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type ResponseFormat int

const (
	ResponseFormatDiagnosis ResponseFormat = iota + 1
	ResponseFormatPlaybookRecommendations
)

type Config struct {
	v1.AIActionClient
	UseAgent       bool
	ResponseFormat ResponseFormat
}

func Prompt(ctx context.Context, config Config, systemPrompt string, promptParts ...string) (string, []llms.MessageContent, error) {
	model, err := getLLMModel(ctx, config)
	if err != nil {
		return "", nil, err
	}

	content := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}

	for _, p := range promptParts {
		content = append(content, llms.TextParts(llms.ChatMessageTypeHuman, p))
	}

	resp, err := model.GenerateContent(ctx, content, llms.WithTemperature(0))
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", nil, errors.New("no response from LLM")
	}

	content = append(content, llms.TextParts(llms.ChatMessageTypeAI, resp.Choices[0].Content))
	return resp.Choices[0].Content, content, nil
}

func PromptWithHistory(ctx context.Context, config Config, history []llms.MessageContent, prompt string) (string, []llms.MessageContent, error) {
	model, err := getLLMModel(ctx, config)
	if err != nil {
		return "", nil, err
	}

	content := append(history, llms.TextParts(llms.ChatMessageTypeHuman, prompt))

	resp, err := model.GenerateContent(ctx, content, llms.WithTemperature(0))
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", nil, errors.New("no response from LLM")
	}

	content = append(content, llms.TextParts(llms.ChatMessageTypeAI, resp.Choices[0].Content))
	return resp.Choices[0].Content, content, nil
}

func getLLMModel(ctx context.Context, config Config) (llms.Model, error) {
	switch config.Backend {
	case api.LLMBackendOpenAI:
		var opts []openai.Option
		if !config.APIKey.IsEmpty() {
			opts = append(opts, openai.WithToken(config.APIKey.ValueStatic))
		}
		if config.APIURL != "" {
			opts = append(opts, openai.WithBaseURL(config.APIURL))
		}
		if config.Model != "" {
			opts = append(opts, openai.WithModel(config.Model))
		}
		if config.ResponseFormat == ResponseFormatDiagnosis {
			openaiResponseFormatOpt := openai.WithResponseFormat(&openai.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &openai.ResponseFormatJSONSchema{
					Name:   "diagnosis",
					Strict: true,
					Schema: &openai.ResponseFormatJSONSchemaProperty{
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
					},
				},
			})
			opts = append(opts, openaiResponseFormatOpt)
		} else if config.ResponseFormat == ResponseFormatPlaybookRecommendations {
			openaiResponseFormatOpt := openai.WithResponseFormat(&openai.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &openai.ResponseFormatJSONSchema{
					Name: "playbook_recommendations",
					Schema: &openai.ResponseFormatJSONSchemaProperty{
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
											Type:                 "object",
											Description:          "A key-value (Record<string, string>) pair of parameters to pass to the playbook. Keep in mind the values are all strings even numbers and booleans are strings",
											AdditionalProperties: true,
										},
										"resource_id": {
											Type: "string",
										},
									},
									Required: []string{"id", "emoji", "name", "parameters", "resource_id"},
								},
							},
						},
						Required: []string{"playbooks"},
					},
				},
			})
			opts = append(opts, openaiResponseFormatOpt)
		}

		openaiLLM, err := openai.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to created openAI llm: %w", err)
		}
		return openaiLLM, nil

	case api.LLMBackendOllama:
		var opts []ollama.Option
		if config.APIURL != "" {
			opts = append(opts, ollama.WithServerURL(config.APIURL))
		}
		if config.Model != "" {
			opts = append(opts, ollama.WithModel(config.Model))
		}

		openaiLLM, err := ollama.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to created ollama llm: %w", err)
		}
		return openaiLLM, nil

	case api.LLMBackendAnthropic:
		var opts []anthropic.Option
		if !config.APIKey.IsEmpty() {
			opts = append(opts, anthropic.WithToken(config.APIKey.ValueStatic))
		}
		if config.APIURL != "" {
			opts = append(opts, anthropic.WithBaseURL(config.APIURL))
		}
		if config.Model != "" {
			opts = append(opts, anthropic.WithModel(config.Model))
		}

		anthropicLLM, err := anthropic.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create Anthropic llm: %w", err)
		}
		return anthropicLLM, nil

	case api.LLMBackendGemini:
		var opts []googleai.Option
		if !config.APIKey.IsEmpty() {
			opts = append(opts, googleai.WithAPIKey(config.APIKey.ValueStatic))
		}
		if config.Model != "" {
			opts = append(opts, googleai.WithDefaultModel(config.Model))
		} else {
			opts = append(opts, googleai.WithDefaultModel("gemini-2.0-flash"))
		}

		googleLLM, err := googleai.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create google gemini llm: %w", err)
		}
		return googleLLM, nil

	default:
		return nil, errors.New("unknown config.Backend")
	}
}
