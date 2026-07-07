package llm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/incident-commander/api"
)

const million = 1_000_000.0

//go:embed pricing_registry.json
var embeddedPricingRegistry []byte

// ContextPriceTier defines price overrides that apply when the prompt context
// exceeds a provider threshold.
type ContextPriceTier struct {
	TokenThreshold   int      `json:"tokenThreshold"`
	InputPrice       *float64 `json:"inputPrice,omitempty"`
	OutputPrice      *float64 `json:"outputPrice,omitempty"`
	CacheWritesPrice *float64 `json:"cacheWritesPrice,omitempty"`
	CacheReadsPrice  *float64 `json:"cacheReadsPrice,omitempty"`
}

// ModelInfo holds pricing information for a model.
type ModelInfo struct {
	Provider            api.LLMBackend     `json:"provider"`
	ModelID             string             `json:"modelID"`
	MaxTokens           int                `json:"maxTokens,omitempty"`
	ContextWindow       int                `json:"contextWindow,omitempty"`
	SupportsImages      bool               `json:"supportsImages,omitempty"`
	SupportsPromptCache bool               `json:"supportsPromptCache,omitempty"`
	InputPrice          float64            `json:"inputPrice"`
	OutputPrice         float64            `json:"outputPrice"`
	CacheWritesPrice    float64            `json:"cacheWritesPrice,omitempty"`
	CacheReadsPrice     float64            `json:"cacheReadsPrice,omitempty"`
	ContextPriceTiers   []ContextPriceTier `json:"contextPriceTiers,omitempty"`
}

type pricingRegistryFile struct {
	Providers map[api.LLMBackend]map[string]ModelInfo `json:"providers"`
	Source    string                                  `json:"source"`
	Version   int                                     `json:"version"`
}

var modelRegistry map[api.LLMBackend]map[string]ModelInfo

func init() {
	registry, err := loadPricingRegistry(embeddedPricingRegistry)
	if err != nil {
		panic(fmt.Errorf("failed to load embedded LLM pricing registry: %w", err))
	}
	modelRegistry = registry
}

func loadPricingRegistry(data []byte) (map[api.LLMBackend]map[string]ModelInfo, error) {
	var registryFile pricingRegistryFile
	if err := json.Unmarshal(data, &registryFile); err != nil {
		return nil, err
	}
	if registryFile.Version != 1 {
		return nil, fmt.Errorf("unsupported pricing registry version: %d", registryFile.Version)
	}
	if len(registryFile.Providers) == 0 {
		return nil, fmt.Errorf("pricing registry has no providers")
	}

	registry := make(map[api.LLMBackend]map[string]ModelInfo, len(registryFile.Providers))
	for provider, models := range registryFile.Providers {
		if !isSupportedPricingProvider(provider) {
			return nil, fmt.Errorf("unsupported pricing provider: %s", provider)
		}
		if len(models) == 0 {
			continue
		}

		registry[provider] = make(map[string]ModelInfo, len(models))
		for modelID, modelInfo := range models {
			modelID = normalizeModelID(modelID)
			if modelID == "" {
				return nil, fmt.Errorf("pricing registry has empty model ID for provider %s", provider)
			}
			if modelInfo.ModelID == "" {
				modelInfo.ModelID = modelID
			}
			if modelInfo.Provider == "" {
				modelInfo.Provider = provider
			}
			if modelInfo.Provider != provider {
				return nil, fmt.Errorf("model %s has provider %s in %s registry", modelID, modelInfo.Provider, provider)
			}
			if modelInfo.InputPrice < 0 || modelInfo.OutputPrice < 0 || modelInfo.CacheReadsPrice < 0 || modelInfo.CacheWritesPrice < 0 {
				return nil, fmt.Errorf("model %s/%s has negative pricing", provider, modelID)
			}
			for _, tier := range modelInfo.ContextPriceTiers {
				if tier.TokenThreshold <= 0 {
					return nil, fmt.Errorf("model %s/%s has invalid context price tier threshold", provider, modelID)
				}
				if ptrFloatNegative(tier.InputPrice) || ptrFloatNegative(tier.OutputPrice) || ptrFloatNegative(tier.CacheReadsPrice) || ptrFloatNegative(tier.CacheWritesPrice) {
					return nil, fmt.Errorf("model %s/%s has negative context tier pricing", provider, modelID)
				}
			}

			registry[provider][modelID] = modelInfo
		}
	}

	return registry, nil
}

func isSupportedPricingProvider(provider api.LLMBackend) bool {
	switch provider {
	case api.LLMBackendAnthropic, api.LLMBackendOpenAI, api.LLMBackendGemini, api.LLMBackendBedrock:
		return true
	default:
		return false
	}
}

// CalculateCost calculates cost from the embedded pricing registry.
func CalculateCost(provider api.LLMBackend, modelID string, genInfo GenerationInfo) (float64, error) {
	modelInfo, err := GetModelInfo(provider, modelID)
	if err != nil {
		return 0, err
	}

	prices := pricesForUsage(modelInfo, genInfo)
	inputCost := float64(genInfo.InputTokens) * prices.Input / million
	outputTokens := genInfo.OutputTokens + ptrInt(genInfo.ReasoningTokens)
	outputCost := float64(outputTokens) * prices.Output / million
	cacheReadCost := float64(ptrInt(genInfo.CacheReadTokens)) * prices.CacheRead / million
	cacheWriteCost := float64(ptrInt(genInfo.CacheWriteTokens)) * prices.CacheWrite / million

	return inputCost + outputCost + cacheReadCost + cacheWriteCost, nil
}

type modelPrices struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

func pricesForUsage(modelInfo ModelInfo, genInfo GenerationInfo) modelPrices {
	prices := modelPrices{
		Input:      modelInfo.InputPrice,
		Output:     modelInfo.OutputPrice,
		CacheRead:  modelInfo.CacheReadsPrice,
		CacheWrite: modelInfo.CacheWritesPrice,
	}

	contextTokens := genInfo.InputTokens + ptrInt(genInfo.CacheReadTokens) + ptrInt(genInfo.CacheWriteTokens)
	var selectedTier *ContextPriceTier
	for i := range modelInfo.ContextPriceTiers {
		tier := &modelInfo.ContextPriceTiers[i]
		if contextTokens > tier.TokenThreshold && (selectedTier == nil || tier.TokenThreshold > selectedTier.TokenThreshold) {
			selectedTier = tier
		}
	}
	if selectedTier == nil {
		return prices
	}

	if selectedTier.InputPrice != nil {
		prices.Input = *selectedTier.InputPrice
	}
	if selectedTier.OutputPrice != nil {
		prices.Output = *selectedTier.OutputPrice
	}
	if selectedTier.CacheReadsPrice != nil {
		prices.CacheRead = *selectedTier.CacheReadsPrice
	}
	if selectedTier.CacheWritesPrice != nil {
		prices.CacheWrite = *selectedTier.CacheWritesPrice
	}

	return prices
}

// GetModelInfo retrieves pricing metadata for an exact provider/model match.
func GetModelInfo(provider api.LLMBackend, modelID string) (ModelInfo, error) {
	modelID = normalizeModelID(modelID)
	providerModels, ok := modelRegistry[provider]
	if ok {
		modelInfo, modelOk := providerModels[modelID]
		if modelOk {
			return modelInfo, nil
		}
	}

	return ModelInfo{}, fmt.Errorf("model not found for provider %q: %s", provider, modelID)
}

func normalizeModelID(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	return strings.TrimPrefix(modelID, "models/")
}

func ptrInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func ptrFloatNegative(value *float64) bool {
	return value != nil && *value < 0
}
