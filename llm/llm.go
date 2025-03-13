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

const (
	forceToolUsePrompt = `You MUST use the %s tool to extract the diagnosis information. 
	Do not provide any other response format. 
	Only use the tool to respond.`
)

func Prompt(ctx context.Context, config Config, systemPrompt string, promptParts ...string) (string, []llms.MessageContent, error) {
	content := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}

	for _, p := range promptParts {
		content = append(content, llms.TextParts(llms.ChatMessageTypeHuman, p))
	}

	return PromptWithHistory(ctx, config, content, "")
}

func PromptWithHistory(ctx context.Context, config Config, history []llms.MessageContent, prompt string) (string, []llms.MessageContent, error) {
	model, err := getLLMModel(ctx, config)
	if err != nil {
		return "", nil, err
	}

	messages := append(history, llms.TextParts(llms.ChatMessageTypeHuman, prompt))

	options := []llms.CallOption{llms.WithTemperature(0)}

	// Add backend-specific options for tool choice
	switch config.Backend {
	case api.LLMBackendOpenAI:
		// Do nothing
		// NOTE: we use `response_format` instead of function calling.
		// OpenAI does support function calling, but I don't think we can force the model to use that tool
		// like we can with Anthropic.

	default:
		if config.ResponseFormat == ResponseFormatDiagnosis {
			options = append(options, llms.WithTools([]llms.Tool{diagnosisTool}))
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, fmt.Sprintf(forceToolUsePrompt, diagnosisTool.Function.Name)))
		} else if config.ResponseFormat == ResponseFormatPlaybookRecommendations {
			options = append(options, llms.WithTools([]llms.Tool{playbookRecommendationsTool}))
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, fmt.Sprintf(forceToolUsePrompt, playbookRecommendationsTool.Function.Name)))
		}

		// NOTE: Anthropic & Gemini have forced tool use, but it's not supported in LangChainGo.
		// So, we force that with prompts for now.
		//
		// https://docs.anthropic.com/en/docs/build-with-claude/tool-use/overview#forcing-tool-use
		// options = append(options, llms.WithToolChoice(map[string]any{
		// 	"type": "tool",
		// 	"name": "extract_diagnosis",
		// }))
		//
		// Use the correct format for Gemini function calling
		// options = append(options, llms.WithToolChoice(map[string]any{
		// 	"function_calling_config": map[string]any{
		// 		"mode":                   "ANY",
		// 		"allowed_function_names": []string{"extract_diagnosis"},
		// 	},
		// }))
	}

	resp, err := model.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", nil, errors.New("no response from LLM")
	}

	aiResponse := resp.Choices[0].Content

	// We prioritize response from tools if available
	for _, choice := range resp.Choices {
		if len(choice.ToolCalls) > 0 {
			aiResponse = choice.ToolCalls[0].FunctionCall.Arguments
			break
		}
	}

	messages = append(messages, llms.TextParts(llms.ChatMessageTypeAI, aiResponse))
	return aiResponse, messages, nil
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
					Schema: &diagnosisToolSchema,
				},
			})
			opts = append(opts, openaiResponseFormatOpt)
		} else if config.ResponseFormat == ResponseFormatPlaybookRecommendations {
			openaiResponseFormatOpt := openai.WithResponseFormat(&openai.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &openai.ResponseFormatJSONSchema{
					Name: "playbook_recommendations",
					// Strict: true, // NOTE: cannot set this to strict, because playbook.parameters must be additionalProperties=true
					Schema: &recommendatationToolSchema,
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
