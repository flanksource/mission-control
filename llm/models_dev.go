package llm

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/flanksource/incident-commander/api"
)

// ModelsDevAPIURL is the default upstream catalog used to generate the embedded pricing registry.
const ModelsDevAPIURL = "https://models.dev/api.json"

var modelsDevProviderMappings = []struct {
	source string
	target api.LLMBackend
}{
	{source: "openai", target: api.LLMBackendOpenAI},
	{source: "anthropic", target: api.LLMBackendAnthropic},
	{source: "google", target: api.LLMBackendGemini},
	{source: "amazon-bedrock", target: api.LLMBackendBedrock},
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	Modalities modelsDevModalities `json:"modalities"`
	Limit      modelsDevLimit      `json:"limit"`
	Cost       *modelsDevCost      `json:"cost"`
}

type modelsDevModalities struct {
	Input []string `json:"input"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type modelsDevCost struct {
	Input      *float64            `json:"input"`
	Output     *float64            `json:"output"`
	CacheRead  *float64            `json:"cache_read"`
	CacheWrite *float64            `json:"cache_write"`
	Tiers      []modelsDevCostTier `json:"tiers"`
}

type modelsDevCostTier struct {
	Input      *float64         `json:"input"`
	Output     *float64         `json:"output"`
	CacheRead  *float64         `json:"cache_read"`
	CacheWrite *float64         `json:"cache_write"`
	Tier       modelsDevTierKey `json:"tier"`
}

type modelsDevTierKey struct {
	Type string `json:"type"`
	Size int    `json:"size"`
}

// GeneratePricingRegistryFromModelsDev converts the models.dev API response into
// the embedded registry format used by CalculateCost.
func GeneratePricingRegistryFromModelsDev(data []byte) ([]byte, error) {
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, fmt.Errorf("parse models.dev catalog: %w", err)
	}

	registryFile := pricingRegistryFile{
		Providers: make(map[api.LLMBackend]map[string]ModelInfo, len(modelsDevProviderMappings)),
		Source:    ModelsDevAPIURL,
		Version:   1,
	}

	for _, mapping := range modelsDevProviderMappings {
		provider, ok := providers[mapping.source]
		if !ok {
			return nil, fmt.Errorf("models.dev catalog is missing provider %q", mapping.source)
		}

		models := make(map[string]ModelInfo, len(provider.Models))
		for modelID, model := range provider.Models {
			modelInfo, ok := modelInfoFromModelsDev(mapping.target, modelID, model)
			if ok {
				models[modelID] = modelInfo
			}
		}
		if len(models) == 0 {
			return nil, fmt.Errorf("models.dev provider %q has no priced models", mapping.source)
		}

		registryFile.Providers[mapping.target] = models
	}

	output, err := json.MarshalIndent(registryFile, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode pricing registry: %w", err)
	}
	output = append(output, '\n')

	if _, err := loadPricingRegistry(output); err != nil {
		return nil, fmt.Errorf("validate pricing registry: %w", err)
	}

	return output, nil
}

func modelInfoFromModelsDev(provider api.LLMBackend, modelID string, model modelsDevModel) (ModelInfo, bool) {
	if model.Cost == nil || model.Cost.Input == nil || model.Cost.Output == nil {
		return ModelInfo{}, false
	}

	modelInfo := ModelInfo{
		Provider:            provider,
		ModelID:             modelID,
		MaxTokens:           model.Limit.Output,
		ContextWindow:       model.Limit.Context,
		SupportsImages:      slices.Contains(model.Modalities.Input, "image"),
		SupportsPromptCache: model.Cost.CacheRead != nil || model.Cost.CacheWrite != nil,
		InputPrice:          *model.Cost.Input,
		OutputPrice:         *model.Cost.Output,
		ContextPriceTiers:   contextPriceTiersFromModelsDev(model.Cost.Tiers),
	}
	if model.Cost.CacheRead != nil {
		modelInfo.CacheReadsPrice = *model.Cost.CacheRead
	}
	if model.Cost.CacheWrite != nil {
		modelInfo.CacheWritesPrice = *model.Cost.CacheWrite
	}

	return modelInfo, true
}

func contextPriceTiersFromModelsDev(tiers []modelsDevCostTier) []ContextPriceTier {
	var contextTiers []ContextPriceTier
	for _, tier := range tiers {
		if tier.Tier.Type != "context" || tier.Tier.Size == 0 {
			continue
		}

		contextTiers = append(contextTiers, ContextPriceTier{
			TokenThreshold:   tier.Tier.Size,
			InputPrice:       tier.Input,
			OutputPrice:      tier.Output,
			CacheReadsPrice:  tier.CacheRead,
			CacheWritesPrice: tier.CacheWrite,
		})
	}

	return contextTiers
}
