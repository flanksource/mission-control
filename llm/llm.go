package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	genkitai "github.com/firebase/genkit/go/ai"
	genkitapi "github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	anthropicplugin "github.com/firebase/genkit/go/plugins/anthropic"
	openaiplugin "github.com/firebase/genkit/go/plugins/compat_oai"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	ollamaplugin "github.com/firebase/genkit/go/plugins/ollama"
	dutyctx "github.com/flanksource/duty/context"
	"github.com/samber/lo"
	bedrockplugin "github.com/xavidop/genkit-aws-bedrock-go"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/llm/tools"
)

type ResponseFormat int

const (
	ResponseFormatDiagnosis ResponseFormat = iota + 1
	ResponseFormatPlaybookRecommendations
	ResponseFormatCustomSchema
)

type Config struct {
	v1.AIActionClient
	UseAgent       bool
	ResponseFormat ResponseFormat
	// CustomSchema is the raw JSON schema string when ResponseFormat == ResponseFormatCustomSchema
	CustomSchema string
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

func Prompt(ctx dutyctx.Context, config Config, systemPrompt string, promptParts ...string) (string, []*genkitai.Message, []GenerationInfo, error) {
	content := []*genkitai.Message{
		genkitai.NewSystemTextMessage(systemPrompt),
	}

	for _, p := range promptParts {
		content = append(content, genkitai.NewUserTextMessage(p))
	}

	return PromptWithHistory(ctx, config, content, "")
}

func PromptWithHistory(ctx dutyctx.Context, config Config, history []*genkitai.Message, prompt string) (string, []*genkitai.Message, []GenerationInfo, error) {
	g, modelName, err := initGenkit(config)
	if err != nil {
		return "", nil, nil, err
	}

	messages := append([]*genkitai.Message{}, history...)
	if prompt != "" {
		messages = append(messages, genkitai.NewUserTextMessage(prompt))
	}

	schema, err := schemaForResponseFormat(config)
	if err != nil {
		return "", nil, nil, err
	}

	opts := []genkitai.GenerateOption{
		genkitai.WithModelName(modelName),
		genkitai.WithMessages(messages...),
	}
	if genConfig := generationConfig(config.Backend, modelName); genConfig != nil {
		opts = append(opts, genkitai.WithConfig(genConfig))
	}
	if schema != nil {
		opts = append(opts, genkitai.WithOutputSchema(schema))
	}

	resp, err := genkit.Generate(ctx, g, opts...)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to generate response: %w", err)
	}

	aiResponse := resp.Text()
	if aiResponse == "" {
		return "", nil, nil, errors.New("no response from LLM")
	}

	messages = resp.History()
	genInfo := calculateGenerationInfo(config.Backend, unqualifiedModelName(modelName), resp)
	return aiResponse, messages, genInfo, nil
}

func initGenkit(config Config) (g *genkit.Genkit, modelName string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to initialize Genkit: %v", r)
		}
	}()

	var provider string
	var plugin genkitapi.Plugin
	var bedrockPlugin *bedrockplugin.Bedrock

	switch config.Backend {
	case api.LLMBackendOpenAI:
		provider = "openai"
		plugin = &openaiplugin.OpenAICompatible{
			Provider: provider,
			APIKey:   config.APIKey.ValueStatic,
			BaseURL:  config.APIURL,
		}

	case api.LLMBackendOllama:
		provider = "ollama"
		serverAddress := config.APIURL
		if serverAddress == "" {
			serverAddress = "http://localhost:11434"
		}
		plugin = &ollamaplugin.Ollama{ServerAddress: serverAddress}

	case api.LLMBackendAnthropic:
		provider = "anthropic"
		plugin = &anthropicplugin.Anthropic{
			APIKey:  config.APIKey.ValueStatic,
			BaseURL: config.APIURL,
		}

	case api.LLMBackendGemini:
		provider = "googleai"
		plugin = &googlegenai.GoogleAI{APIKey: config.APIKey.ValueStatic}

	case api.LLMBackendBedrock:
		provider = "bedrock"
		bedrockCfg, cfgErr := newBedrockAWSConfig(config)
		if cfgErr != nil {
			return nil, "", cfgErr
		}
		bedrockPlugin = &bedrockplugin.Bedrock{
			AWSConfig: &bedrockCfg,
		}
		plugin = bedrockPlugin

	default:
		return nil, "", errors.New("unknown config.Backend")
	}

	model := defaultModel(config.Backend, config.Model)
	modelName, err = qualifyModelName(provider, model)
	if err != nil {
		return nil, "", err
	}

	// Use context.Background() so the genkit lifecycle isn't tied to any request context.
	g = genkit.Init(context.Background(), genkit.WithPlugins(plugin))

	// Bedrock requires explicit model registration via DefineModel after Init.
	if bedrockPlugin != nil {
		bedrockPlugin.DefineModel(g, bedrockplugin.ModelDefinition{
			Name: model,
			Type: "chat",
		}, nil)
	}

	return g, modelName, nil
}

func newBedrockAWSConfig(cfg Config) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.AWSRegion != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.AWSRegion))
	}
	if cfg.AWSAccessKey.ValueStatic != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.AWSAccessKey.ValueStatic,
				cfg.APIKey.ValueStatic,
				"",
			),
		))
	}
	return awsconfig.LoadDefaultConfig(context.Background(), opts...)
}

func generationConfig(backend api.LLMBackend, model string) map[string]any {
	config := map[string]any{}

	// GPT-5.5 only accepts the default temperature, so omit temperature instead of sending 0.
	if backend != api.LLMBackendOpenAI || !isOpenAIDefaultTemperatureOnly(model) {
		config["temperature"] = 0
	}
	if backend == api.LLMBackendAnthropic || backend == api.LLMBackendBedrock {
		config["max_tokens"] = 2048
	}
	if len(config) == 0 {
		return nil
	}
	return config
}

func isOpenAIDefaultTemperatureOnly(model string) bool {
	model = unqualifiedModelName(model)
	return model == "gpt-5.5" || strings.HasPrefix(model, "gpt-5.5-")
}

func defaultModel(backend api.LLMBackend, model string) string {
	if strings.TrimSpace(model) != "" {
		return model
	}

	switch backend {
	case api.LLMBackendOpenAI:
		return "gpt-4o-mini"
	case api.LLMBackendAnthropic:
		return "claude-3-5-sonnet-latest"
	case api.LLMBackendGemini:
		return "gemini-2.5-pro-exp-03-25"
	case api.LLMBackendBedrock:
		return "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
	default:
		return model
	}
}

func qualifyModelName(provider, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("llm model is required for backend %q", provider)
	}

	for _, knownProvider := range []string{"anthropic", "bedrock", "googleai", "ollama", "openai", "vertexai"} {
		if strings.HasPrefix(model, knownProvider+"/") {
			return model, nil
		}
	}

	return provider + "/" + strings.TrimPrefix(model, "models/"), nil
}

func unqualifiedModelName(model string) string {
	model = strings.TrimSpace(model)
	if i := strings.Index(model, "/"); i >= 0 && i < len(model)-1 {
		return model[i+1:]
	}
	return strings.TrimPrefix(model, "models/")
}

func schemaForResponseFormat(config Config) (map[string]any, error) {
	switch config.ResponseFormat {
	case ResponseFormatDiagnosis:
		return tools.ExtractDiagnosisToolSchema, nil
	case ResponseFormatPlaybookRecommendations:
		return tools.RecommendPlaybooksToolSchema, nil
	case ResponseFormatCustomSchema:
		return tools.CustomSchema(config.CustomSchema)
	default:
		return nil, nil
	}
}

func calculateGenerationInfo(llmBackend api.LLMBackend, model string, resp *genkitai.ModelResponse) []GenerationInfo {
	if resp == nil || resp.Usage == nil {
		return nil
	}

	genInfo := GenerationInfo{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		Model:        model,
	}
	if resp.Usage.ThoughtsTokens > 0 {
		genInfo.ReasoningTokens = lo.ToPtr(resp.Usage.ThoughtsTokens)
	}
	if resp.Usage.CachedContentTokens > 0 {
		genInfo.CacheReadTokens = lo.ToPtr(resp.Usage.CachedContentTokens)
	}
	if n, ok := resp.Usage.Custom["cacheCreationInputTokens"]; ok && n > 0 {
		genInfo.CacheWriteTokens = lo.ToPtr(int(n))
	}

	cost, err := CalculateCost(llmBackend, model, genInfo)
	if err != nil {
		genInfo.CostCalculationError = lo.ToPtr(err.Error())
	} else {
		genInfo.Cost = cost
	}

	return []GenerationInfo{genInfo}
}
