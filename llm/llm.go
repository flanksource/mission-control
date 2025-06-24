package llm

import (
	"errors"
	"fmt"

	dutyctx "github.com/flanksource/duty/context"
	"github.com/samber/lo"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"google.golang.org/genai"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/llm/tools"
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

type GenerationInfo struct {
	InputTokens          int     `json:"inputTokens"`
	OutputTokens         int     `json:"outputTokens"`
	ReasoningTokens      *int    `json:"reasoningTokens,omitempty"`
	CacheReadTokens      *int    `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens     *int    `json:"cacheWriteTokens,omitempty"`
	Cost                 float64 `json:"cost"`
	CostCalculationError *string `json:"costCalculationError,omitempty"`
	Model                string  `json:"model"`
}

func Prompt(ctx dutyctx.Context, config Config, systemPrompt string, promptParts ...string) (string, []llms.MessageContent, []GenerationInfo, error) {
	content := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}

	for _, p := range promptParts {
		content = append(content, llms.TextParts(llms.ChatMessageTypeHuman, p))
	}

	return PromptWithHistory(ctx, config, content, "")
}

func PromptWithHistory(ctx dutyctx.Context, config Config, history []llms.MessageContent, prompt string) (string, []llms.MessageContent, []GenerationInfo, error) {
	model, err := getLLMModel(ctx, config)
	if err != nil {
		return "", nil, nil, err
	}

	var messages []llms.MessageContent
	messages = append(messages, history...)
	if prompt != "" {
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, prompt))
	}

	options := []llms.CallOption{llms.WithTemperature(0)}

	// Add backend-specific options for tool choice
	switch config.Backend {
	case api.LLMBackendOpenAI:
		// Do nothing
		// NOTE: we use `response_format` instead of function calling & that's configured during model creation not when prompting.
		// OpenAI does support function calling, but I don't think we can force the model to use that tool
		// like we can with Anthropic.

	case api.LLMBackendGemini:
		// Do nothing
		// NOTE: Handled by the wrapper

	default:
		const forceToolUsePrompt = `You MUST use the %s tool to extract the diagnosis information.
	Do not provide any other response format.
	Only use the tool to respond.`

		switch config.ResponseFormat {
		case ResponseFormatDiagnosis:
			options = append(options, llms.WithTools([]llms.Tool{tools.ExtractDiagnosis}))
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, fmt.Sprintf(forceToolUsePrompt, tools.ToolExtractDiagnosis)))
		case ResponseFormatPlaybookRecommendations:
			options = append(options, llms.WithTools([]llms.Tool{tools.RecommendPlaybook}))
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, fmt.Sprintf(forceToolUsePrompt, tools.ToolPlaybookRecommendations)))
		}
		// NOTE: Anthropic has forced tool use, but it's not supported in LangChainGo.
		// So, we force that with prompts for now.
		//
		// https://docs.anthropic.com/en/docs/build-with-claude/tool-use/overview#forcing-tool-use
		// options = append(options, llms.WithToolChoice(map[string]any{
		// 	"type": "tool",
		// 	"name": "extract_diagnosis",
		// }))
	}

	resp, err := model.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to generate response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", nil, nil, errors.New("no response from LLM")
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
	genInfo := calculateGenerationInfo(config.Backend, config.Model, resp)
	return aiResponse, messages, genInfo, nil
}

func calculateGenerationInfo(llmBackend api.LLMBackend, model string, resp *llms.ContentResponse) []GenerationInfo {
	var generationInfoList []GenerationInfo
	for _, choice := range resp.Choices {
		if choice.GenerationInfo != nil {
			genInfo := GenerationInfo{
				Model: model,
			}

			switch llmBackend {
			case api.LLMBackendOpenAI:
				if inputTokens, ok := choice.GenerationInfo["PromptTokens"]; ok {
					genInfo.InputTokens += inputTokens.(int)
				}
				if outputTokens, ok := choice.GenerationInfo["CompletionTokens"]; ok {
					genInfo.OutputTokens += outputTokens.(int)
				}
				if reasoningTokens, ok := choice.GenerationInfo["ReasoningTokens"]; ok {
					genInfo.ReasoningTokens = lo.ToPtr(reasoningTokens.(int))
				}

			case api.LLMBackendAnthropic, api.LLMBackendBedrock:
				if inputTokens, ok := choice.GenerationInfo["InputTokens"]; ok {
					genInfo.InputTokens += inputTokens.(int)
				}
				if outputTokens, ok := choice.GenerationInfo["OutputTokens"]; ok {
					genInfo.OutputTokens += outputTokens.(int)
				}

			case api.LLMBackendGemini:
				if inputTokens, ok := choice.GenerationInfo["InputTokens"]; ok {
					genInfo.InputTokens += int(inputTokens.(int32))
				}
				if outputTokens, ok := choice.GenerationInfo["OutputTokens"]; ok {
					genInfo.OutputTokens += int(outputTokens.(int32))
				}
			}

			cost, err := CalculateCost(llmBackend, model, genInfo)
			if err != nil {
				genInfo.CostCalculationError = lo.ToPtr(err.Error())
			} else {
				genInfo.Cost = cost
			}

			generationInfoList = append(generationInfoList, genInfo)
		}

		if llmBackend == api.LLMBackendAnthropic || llmBackend == api.LLMBackendBedrock {
			// For Anthropic and Bedrock, only use the first choice to avoid double-counting.
			break
		}
	}

	return generationInfoList
}

func getLLMModel(ctx dutyctx.Context, config Config) (llms.Model, error) {
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

		switch config.ResponseFormat {
		case ResponseFormatDiagnosis, ResponseFormatPlaybookRecommendations:
			schemaName := "diagnosis"
			schemaObj := &tools.ExtractDiagnosisToolSchema

			if config.ResponseFormat == ResponseFormatPlaybookRecommendations {
				schemaName = "playbook_recommendations"
				schemaObj = &tools.RecommendPlaybooksToolSchema
			}

			opts = append(opts, openai.WithResponseFormat(&openai.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &openai.ResponseFormatJSONSchema{
					Name:   schemaName,
					Strict: true,
					Schema: schemaObj,
				},
			}))
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
		apiKey := config.APIKey.ValueStatic
		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini client: %w", err)
		}

		// Create a wrapper that implements the langchaingo Model interface
		wrapper := &GeminiModelWrapper{
			model:          config.Model,
			client:         client,
			ResponseFormat: config.ResponseFormat,
		}

		return wrapper, nil

	case api.LLMBackendBedrock:
		region := config.APIURL // optional, may be empty
		modelID := config.Model
		if modelID == "" {
			modelID = "anthropic.claude-v2"
		}
		wrapper, err := NewBedrockModelWrapper(ctx, modelID, region, config.ResponseFormat)
		return wrapper, err

	default:
		return nil, errors.New("unknown config.Backend")
	}
}
}
