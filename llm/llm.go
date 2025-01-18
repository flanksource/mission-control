package llm

import (
	"errors"
	"fmt"
	"strings"

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
)

type Config struct {
	v1.AIActionClient
	PlaybookAgents []byte
}

func Prompt(ctx context.Context, config Config, systemPrompt string, promptParts []string, agentTools ...tools.Tool) (string, []llms.MessageContent, error) {
	model, err := getLLMModel(config)
	if err != nil {
		return "", nil, err
	}

	if len(config.PlaybookAgents) != 0 {
		agent := agents.NewOneShotAgent(model,
			agentTools,
			agents.WithMaxIterations(1),
		)

		// TODO: Find a better way to pass in the prompt than just string concat
		playbookPrompt := fmt.Sprintf(`
		You can call the following playbooks to take the fitting action to resolve the issue at hand.
		<playbooks>
		%s
		</playbooks>

		Once you execute the suitable playbook, analyze the result of the playbook run to ensure if it solved our problem.
		If it did, please mention which playbook was used to solve this problem and summarize the fix.`, string(config.PlaybookAgents))
		promptParts = append(promptParts, playbookPrompt)
		prompt := strings.Join(promptParts, "\n")

		executor := agents.NewExecutor(agent)
		response, err := chains.Run(ctx, executor, prompt, chains.WithTemperature(0))
		if err != nil {
			return "", nil, err
		}

		return response, nil, err
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
	model, err := getLLMModel(config)
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
