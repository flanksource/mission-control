package llm

import (
	"errors"
	"fmt"

	dutyctx "github.com/flanksource/duty/context"
	"github.com/google/generative-ai-go/genai"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"google.golang.org/api/option"

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

func Prompt(ctx dutyctx.Context, config Config, systemPrompt string, promptParts ...string) (string, []llms.MessageContent, error) {
	content := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}

	for _, p := range promptParts {
		content = append(content, llms.TextParts(llms.ChatMessageTypeHuman, p))
	}

	return PromptWithHistory(ctx, config, content, "")
}

func PromptWithHistory(ctx dutyctx.Context, config Config, history []llms.MessageContent, prompt string) (string, []llms.MessageContent, error) {
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
		// NOTE: we use `response_format` instead of function calling & that's configured during model creation not when prompting.
		// OpenAI does support function calling, but I don't think we can force the model to use that tool
		// like we can with Anthropic.

	case api.LLMBackendGemini:
		// Do nothing
		// NOTE: Handled by the wrapper
		// Tools are configured during model creation not when prompting.

	default:
		const forceToolUsePrompt = `You MUST use the %s tool to extract the diagnosis information. 
	Do not provide any other response format. 
	Only use the tool to respond.`

		if config.ResponseFormat == ResponseFormatDiagnosis {
			options = append(options, llms.WithTools([]llms.Tool{tools.ExtractDiagnosis}))
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, fmt.Sprintf(forceToolUsePrompt, tools.ToolExtractDiagnosis)))
		} else if config.ResponseFormat == ResponseFormatPlaybookRecommendations {
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
		if config.ResponseFormat == ResponseFormatDiagnosis {
			openaiResponseFormatOpt := openai.WithResponseFormat(&openai.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &openai.ResponseFormatJSONSchema{
					Name:   "diagnosis",
					Strict: true,
					Schema: &tools.ExtractDiagnosisToolSchema,
				},
			})
			opts = append(opts, openaiResponseFormatOpt)
		} else if config.ResponseFormat == ResponseFormatPlaybookRecommendations {
			openaiResponseFormatOpt := openai.WithResponseFormat(&openai.ResponseFormat{
				Type: "json_schema",
				JSONSchema: &openai.ResponseFormatJSONSchema{
					Name:   "playbook_recommendations",
					Strict: true,
					Schema: &tools.RecommendPlaybooksToolSchema,
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
		apiKey := config.APIKey.ValueStatic
		model := config.Model
		if model == "" {
			model = "gemini-2.0-flash"
		}

		client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini client: %w", err)
		}

		genModel := client.GenerativeModel(model)

		if config.ResponseFormat == ResponseFormatDiagnosis {
			genModel.Tools = []*genai.Tool{tools.GeminiDiagnosisTool}

			// Force tool use
			genModel.ToolConfig = &genai.ToolConfig{
				FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode:                 genai.FunctionCallingAny,
					AllowedFunctionNames: []string{tools.ToolExtractDiagnosis},
				},
			}
		} else if config.ResponseFormat == ResponseFormatPlaybookRecommendations {
			genModel.Tools = []*genai.Tool{tools.GeminiRecommendPlaybookTool}

			// Force tool use
			genModel.ToolConfig = &genai.ToolConfig{
				FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode:                 genai.FunctionCallingAny,
					AllowedFunctionNames: []string{tools.ToolPlaybookRecommendations},
				},
			}
		}

		// Create a wrapper that implements the langchaingo Model interface
		return &GeminiModelWrapper{
			model: genModel,
		}, nil

	default:
		return nil, errors.New("unknown config.Backend")
	}
}
