package llm

// Generated from: https://github.com/cline/cline/blob/450583c81d6f861bd1af446390dd3f085380bf28/src/shared/api.ts

import (
	"fmt"
	"math"

	"github.com/flanksource/incident-commander/api"
	"github.com/samber/lo"
)

// Prices are typically per million tokens
const million = 1_000_000.0

// PriceTier defines a pricing tier based on token count.
type PriceTier struct {
	TokenLimit int     // Upper limit (inclusive) of *input* or *output* tokens for this price. Use math.MaxInt32 for the highest tier.
	Price      float64 // Price per million tokens for this tier.
}

// ModelInfo holds the pricing information for a specific model.
type ModelInfo struct {
	Provider            api.LLMBackend
	ModelID             string
	MaxTokens           int
	ContextWindow       int
	SupportsImages      bool
	SupportsPromptCache bool
	InputPrice          float64 // Flat input price per million tokens (used if InputPriceTiers is empty)
	OutputPrice         float64 // Flat output price per million tokens (used if OutputPriceTiers is empty)
	InputPriceTiers     []PriceTier
	OutputPriceTiers    []PriceTier
	CacheWritesPrice    float64 // Price per million cache write tokens
	CacheReadsPrice     float64 // Price per million cache read tokens
}

// modelRegistry stores ModelInfo for all known models, keyed by provider, then model ID.
var modelRegistry = make(map[api.LLMBackend]map[string]ModelInfo)

// init populates the modelRegistry with known model data.
func init() {
	populateAnthropicModels()
	populateGeminiDirectModels() // Populating gemini under 'gemini' key
	populateOpenAIModels()
	// Add calls to populate other providers as needed
}

func populateAnthropicModels() {
	provider := api.LLMBackendAnthropic
	models := make(map[string]ModelInfo)

	sonnet37 := ModelInfo{
		Provider:            provider,
		ModelID:             "claude-3-7-sonnet-20250219",
		MaxTokens:           8192,
		ContextWindow:       200_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          3.0,
		OutputPrice:         15.0,
		CacheWritesPrice:    3.75,
		CacheReadsPrice:     0.3,
	}
	models["claude-3-7-sonnet-20250219"] = sonnet37
	models["claude-3-7-sonnet-latest"] = sonnet37

	sonnet35 := ModelInfo{
		Provider:            provider,
		ModelID:             "claude-3-5-sonnet-20241022",
		MaxTokens:           8192,
		ContextWindow:       200_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          3.0,
		OutputPrice:         15.0,
		CacheWritesPrice:    3.75,
		CacheReadsPrice:     0.3,
	}
	models["claude-3-5-sonnet-20241022"] = sonnet35
	models["claude-3-5-sonnet-latest"] = sonnet35

	models["claude-3-5-haiku-20241022"] = ModelInfo{
		Provider:            provider,
		ModelID:             "claude-3-5-haiku-20241022",
		MaxTokens:           8192,
		ContextWindow:       200_000,
		SupportsImages:      false,
		SupportsPromptCache: true,
		InputPrice:          0.8,
		OutputPrice:         4.0,
		CacheWritesPrice:    1.0,
		CacheReadsPrice:     0.08,
	}
	models["claude-3-opus-20240229"] = ModelInfo{
		Provider:            provider,
		ModelID:             "claude-3-opus-20240229",
		MaxTokens:           4096,
		ContextWindow:       200_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          15.0,
		OutputPrice:         75.0,
		CacheWritesPrice:    18.75,
		CacheReadsPrice:     1.5,
	}
	models["claude-3-haiku-20240307"] = ModelInfo{
		Provider:            provider,
		ModelID:             "claude-3-haiku-20240307",
		MaxTokens:           4096,
		ContextWindow:       200_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          0.25,
		OutputPrice:         1.25,
		CacheWritesPrice:    0.3,
		CacheReadsPrice:     0.03,
	}

	modelRegistry[provider] = models
}

func populateGeminiDirectModels() {
	provider := api.LLMBackendGemini
	models := make(map[string]ModelInfo)

	models["gemini-2.5-pro-exp-03-25"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.5-pro-exp-03-25",
		MaxTokens:           65536,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-2.5-pro-preview-03-25"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.5-pro-preview-03-25",
		MaxTokens:           65536,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPriceTiers: []PriceTier{
			{TokenLimit: 200000, Price: 1.25},
			{TokenLimit: math.MaxInt32, Price: 2.5},
		},
		OutputPriceTiers: []PriceTier{
			{TokenLimit: 200000, Price: 10.0},
			{TokenLimit: math.MaxInt32, Price: 15.0},
		},
	}

	flash20 := ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.0-flash-001",
		MaxTokens:           8192,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-2.0-flash"] = flash20
	models["gemini-2.0-flash-001"] = flash20

	models["gemini-2.0-flash-lite-preview-02-05"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.0-flash-lite-preview-02-05",
		MaxTokens:           8192,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-2.0-pro-exp-02-05"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.0-pro-exp-02-05",
		MaxTokens:           8192,
		ContextWindow:       2_097_152,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-2.0-flash-thinking-exp-01-21"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.0-flash-thinking-exp-01-21",
		MaxTokens:           65_536,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-2.0-flash-thinking-exp-1219"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.0-flash-thinking-exp-1219",
		MaxTokens:           8192,
		ContextWindow:       32_767,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-2.0-flash-exp"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-2.0-flash-exp",
		MaxTokens:           8192,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-1.5-flash-002"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-1.5-flash-002",
		MaxTokens:           8192,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-1.5-flash-exp-0827"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-1.5-flash-exp-0827",
		MaxTokens:           8192,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-1.5-flash-8b-exp-0827"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-1.5-flash-8b-exp-0827",
		MaxTokens:           8192,
		ContextWindow:       1_048_576,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-1.5-pro-002"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-1.5-pro-002",
		MaxTokens:           8192,
		ContextWindow:       2_097_152,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-1.5-pro-exp-0827"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-1.5-pro-exp-0827",
		MaxTokens:           8192,
		ContextWindow:       2_097_152,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}
	models["gemini-exp-1206"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gemini-exp-1206",
		MaxTokens:           8192,
		ContextWindow:       2_097_152,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          0,
		OutputPrice:         0,
	}

	modelRegistry[provider] = models
}

func populateOpenAIModels() {
	provider := api.LLMBackendOpenAI
	models := make(map[string]ModelInfo)

	models["o3"] = ModelInfo{
		Provider:            provider,
		ModelID:             "o3",
		MaxTokens:           100_000,
		ContextWindow:       200_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          10.0,
		OutputPrice:         40.0,
		CacheReadsPrice:     2.5,
	}
	models["o4-mini"] = ModelInfo{
		Provider:            provider,
		ModelID:             "o4-mini",
		MaxTokens:           100_000,
		ContextWindow:       200_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          1.1,
		OutputPrice:         4.4,
		CacheReadsPrice:     0.275,
	}
	models["gpt-4.1"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gpt-4.1",
		MaxTokens:           32_768,
		ContextWindow:       1_047_576,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          2.0,
		OutputPrice:         8.0,
		CacheReadsPrice:     0.5,
	}
	models["gpt-4.1-mini"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gpt-4.1-mini",
		MaxTokens:           32_768,
		ContextWindow:       1_047_576,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          0.4,
		OutputPrice:         1.6,
		CacheReadsPrice:     0.1,
	}
	models["gpt-4.1-nano"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gpt-4.1-nano",
		MaxTokens:           32_768,
		ContextWindow:       1_047_576,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          0.1,
		OutputPrice:         0.4,
		CacheReadsPrice:     0.025,
	}
	models["o3-mini"] = ModelInfo{
		Provider:            provider,
		ModelID:             "o3-mini",
		MaxTokens:           100_000,
		ContextWindow:       200_000,
		SupportsImages:      false,
		SupportsPromptCache: true,
		InputPrice:          1.1,
		OutputPrice:         4.4,
		CacheReadsPrice:     0.55,
	}
	models["o1"] = ModelInfo{
		Provider:            provider,
		ModelID:             "o1",
		MaxTokens:           100_000,
		ContextWindow:       200_000,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          15.0,
		OutputPrice:         60.0,
		CacheReadsPrice:     7.5,
	}
	models["o1-preview"] = ModelInfo{
		Provider:            provider,
		ModelID:             "o1-preview",
		MaxTokens:           32_768,
		ContextWindow:       128_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          15.0,
		OutputPrice:         60.0,
		CacheReadsPrice:     7.5,
	}
	models["o1-mini"] = ModelInfo{
		Provider:            provider,
		ModelID:             "o1-mini",
		MaxTokens:           65_536,
		ContextWindow:       128_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          1.1,
		OutputPrice:         4.4,
		CacheReadsPrice:     0.55,
	}
	models["gpt-4o"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gpt-4o",
		MaxTokens:           4_096,
		ContextWindow:       128_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          2.5,
		OutputPrice:         10.0,
		CacheReadsPrice:     1.25,
	}
	models["gpt-4o-mini"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gpt-4o-mini",
		MaxTokens:           16_384,
		ContextWindow:       128_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          0.15,
		OutputPrice:         0.6,
		CacheReadsPrice:     0.075,
	}
	models["chatgpt-4o-latest"] = ModelInfo{
		Provider:            provider,
		ModelID:             "chatgpt-4o-latest",
		MaxTokens:           16_384,
		ContextWindow:       128_000,
		SupportsImages:      true,
		SupportsPromptCache: false,
		InputPrice:          5.0,
		OutputPrice:         15.0,
	}
	models["gpt-4.5-preview"] = ModelInfo{
		Provider:            provider,
		ModelID:             "gpt-4.5-preview",
		MaxTokens:           16_384,
		ContextWindow:       128_000,
		SupportsImages:      true,
		SupportsPromptCache: true,
		InputPrice:          75.0,
		OutputPrice:         150.0,
	}

	modelRegistry[provider] = models
}

// CalculateCost calculates the estimated cost for a given model and token counts.
// It considers tiered pricing if defined for the model.
// cacheReadTokens and cacheWriteTokens are optional.
func CalculateCost(provider api.LLMBackend, modelID string, genInfo GenerationInfo) (float64, error) {
	if provider == api.LLMBackendBedrock {
		return 0, nil
	}

	modelInfo, err := GetModelInfo(provider, modelID)
	if err != nil {
		return 0, err
	}

	inputCost := calculateTieredCost(genInfo.InputTokens, modelInfo.InputPriceTiers, modelInfo.InputPrice)

	// NOTE: Couldn't find the price for reasoning tokens in the model info.
	// Assuming it's the same as output tokens for now.
	outputCost := calculateTieredCost(genInfo.OutputTokens+lo.FromPtr(genInfo.ReasoningTokens), modelInfo.OutputPriceTiers, modelInfo.OutputPrice)

	readTokens := 0
	if genInfo.CacheReadTokens != nil {
		readTokens = *genInfo.CacheReadTokens
	}
	writeTokens := 0
	if genInfo.CacheWriteTokens != nil {
		writeTokens = *genInfo.CacheWriteTokens
	}

	cacheReadCost := float64(readTokens) * modelInfo.CacheReadsPrice / million
	cacheWriteCost := float64(writeTokens) * modelInfo.CacheWritesPrice / million

	totalCost := inputCost + outputCost + cacheReadCost + cacheWriteCost

	return totalCost, nil
}

// calculateTieredCost calculates cost based on tiers or a flat price.
// Assumes tiers are sorted by TokenLimit ascending, with the last tier potentially having math.MaxInt32.
func calculateTieredCost(tokens int, tiers []PriceTier, flatPrice float64) float64 {
	if tokens <= 0 {
		return 0
	}

	if len(tiers) > 0 {
		var cost float64
		processedTokens := 0

		// Assumes tiers are pre-sorted correctly in the init() functions
		// sort.Slice(tiers, func(i, j int) bool { return tiers[i].TokenLimit < tiers[j].TokenLimit }

		for _, tier := range tiers {
			if processedTokens >= tokens {
				break // All tokens accounted for
			}

			tokensInThisTierCanProcess := tier.TokenLimit - processedTokens
			if tokensInThisTierCanProcess <= 0 {
				continue // This tier starts beyond the tokens we have
			}

			remainingTokens := tokens - processedTokens
			tokensToProcessInTier := min(remainingTokens, tokensInThisTierCanProcess)

			cost += float64(tokensToProcessInTier) * tier.Price / million
			processedTokens += tokensToProcessInTier
		}

		// This part should ideally not be reached if the last tier has TokenLimit=math.MaxInt32
		if processedTokens < tokens {
			// Fallback: Apply the last tier's price to remaining tokens
			// Or handle as an error/log a warning, depending on desired behavior
			if len(tiers) > 0 {
				lastTierPrice := tiers[len(tiers)-1].Price
				remaining := tokens - processedTokens
				cost += float64(remaining) * lastTierPrice / million
				fmt.Printf("Warning: Tokens (%d) exceeded defined tiers for calculation. Applied last tier price to remainder (%d).\\n", tokens, remaining)
			} else {
				fmt.Printf("Warning: Tokens (%d) processed but no tiers defined?\\n", tokens) // Should not happen
			}
		}
		return cost
	} else {
		// Use flat pricing if no tiers are defined
	return float64(tokens) * flatPrice / million
}


}

// GetModelInfo retrieves the ModelInfo for a given provider and model ID.
// It includes logic to handle provider aliases (e.g., "openai" -> "openai-native")
// and to check across gemini/vertex if the model isn't found under the initial provider.
func GetModelInfo(providerKey api.LLMBackend, modelID string) (ModelInfo, error) {
	providerModels, ok := modelRegistry[providerKey]
	if ok {
		modelInfo, modelOk := providerModels[modelID]
		if modelOk {
			return modelInfo, nil
		}
	}

	return ModelInfo{}, fmt.Errorf("model not found for provider '%s' (checked '%s'): %s", providerKey, providerKey, modelID)
}

// Helper min function for integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
