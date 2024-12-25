package llm

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/tools"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	mcTools "github.com/flanksource/incident-commander/llm/tools"
)

type Config struct {
	v1.AIActionClient
	UseAgent bool
}

func Prompt(ctx context.Context, config Config, systemPrompt, prompt string) (string, error) {
	model, err := getLLMModel(config)
	if err != nil {
		return "", err
	}

	if config.UseAgent {
		agentTools := []tools.Tool{
			mcTools.NewCatalogTool(ctx),
		}

		agent := agents.NewOneShotAgent(model,
			agentTools,
			agents.WithMaxIterations(3),
		)

		executor := agents.NewExecutor(agent)
		return chains.Run(ctx, executor, prompt, chains.WithTemperature(0))
	}

	content := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
		llms.TextParts(llms.ChatMessageTypeHuman, prompt),
	}

	resp, err := model.GenerateContent(ctx, content, llms.WithTemperature(0))
	if err != nil {
		return "", fmt.Errorf("failed to generate resposne: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", errors.New("no response from LLM")
	}

	return resp.Choices[0].Content, nil
}

func getLLMModel(config Config) (llms.Model, error) {
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
			return nil, fmt.Errorf("failed to created Anthropic llm: %w", err)
		}
		return anthropicLLM, nil

	default:
		return nil, errors.New("unknown config.Backend")
	}
}