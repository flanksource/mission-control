package llm

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/tools"

	"github.com/flanksource/incident-commander/api"
	mcTools "github.com/flanksource/incident-commander/llm/tools"
)

type Config struct {
	Backend  api.LLMBackend
	Model    string
	APIKey   string
	UseAgent bool
}

func Prompt(ctx context.Context, config Config, prompt string) (string, error) {
	model, err := getLLMModel(config)
	if err != nil {
		return "", err
	}

	if !config.UseAgent {
		return llms.GenerateFromSinglePrompt(ctx, model, prompt, llms.WithTemperature(0))
	}

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

func getLLMModel(config Config) (llms.Model, error) {
	switch config.Backend {
	case api.LLMBackendOpenAI:
		var opts []openai.Option
		if config.APIKey != "" {
			opts = append(opts, openai.WithToken(config.APIKey))
		}
		if config.Model != "" {
			opts = append(opts, openai.WithModel(config.Model))
		}

		openaiLLM, err := openai.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to created openAI llm: %w", err)
		}
		return openaiLLM, nil

	case api.LLMBackendAnthropic:
		var opts []anthropic.Option
		if config.APIKey != "" {
			opts = append(opts, anthropic.WithToken(config.APIKey))
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
