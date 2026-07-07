package llm

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/incident-commander/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("CalculateCost", func() {
	type testCase struct {
		provider     api.LLMBackend
		model        string
		inputTokens  int
		outputTokens int
		expectedCost float64
	}

	// Reference: https://gptforwork.com/tools/openai-chatgpt-api-pricing-calculator
	tests := []testCase{
		{
			provider:     api.LLMBackendOpenAI,
			model:        "o1",
			inputTokens:  15000,
			outputTokens: 20000,
			expectedCost: 1.425,
		},
		{
			provider:     api.LLMBackendOpenAI,
			model:        "gpt-4o",
			inputTokens:  15000,
			outputTokens: 20000,
			expectedCost: 0.2375,
		},
		{
			provider:     api.LLMBackendAnthropic,
			model:        "claude-sonnet-4-6",
			inputTokens:  15000,
			outputTokens: 20000,
			expectedCost: 0.3450,
		},
	}

	for _, test := range tests {
		ginkgo.It(fmt.Sprintf("%s %s", test.provider, test.model), func() {
			cost, err := CalculateCost(test.provider, test.model, GenerationInfo{
				InputTokens:  test.inputTokens,
				OutputTokens: test.outputTokens,
			})
			Expect(err).To(BeNil())
			Expect(cost).To(BeNumerically("~", test.expectedCost, 0.0001), "expected to be equal to 4 decimal places")
		})
	}

	ginkgo.It("applies context threshold pricing", func() {
		cost, err := CalculateCost(api.LLMBackendOpenAI, "gpt-5.5", GenerationInfo{
			InputTokens:  300000,
			OutputTokens: 1000,
		})
		Expect(err).To(BeNil())
		Expect(cost).To(BeNumerically("~", 3.045, 0.0001))
	})

	ginkgo.It("returns an error for unknown models", func() {
		cost, err := CalculateCost(api.LLMBackendAnthropic, "claude-not-real", GenerationInfo{
			InputTokens:  15000,
			OutputTokens: 20000,
		})
		Expect(err).ToNot(BeNil())
		Expect(cost).To(BeZero())
	})

	ginkgo.It("generates the embedded registry from models.dev data", func() {
		source := []byte(`{
			"openai": {
				"models": {
					"gpt-test": {
						"modalities": {"input": ["text", "image"]},
						"limit": {"context": 128000, "output": 16384},
						"cost": {
							"input": 2.5,
							"output": 10,
							"cache_read": 1.25,
							"tiers": [
								{
									"input": 5,
									"output": 20,
									"cache_read": 2,
									"tier": {"type": "context", "size": 200000}
								}
							]
						}
					},
					"gpt-free": {
						"modalities": {"input": ["text"]},
						"limit": {"context": 128000, "output": 16384},
						"cost": {"input": 0, "output": 0}
					},
					"gpt-unpriced": {
						"cost": {"input": 1}
					}
				}
			},
			"anthropic": {
				"models": {
					"claude-test": {
						"modalities": {"input": ["text"]},
						"limit": {"context": 200000, "output": 64000},
						"cost": {"input": 3, "output": 15, "cache_write": 3.75}
					}
				}
			},
			"google": {
				"models": {
					"gemini-test": {
						"modalities": {"input": ["text", "image"]},
						"limit": {"context": 1048576, "output": 65536},
						"cost": {"input": 1.25, "output": 10}
					}
				}
			},
			"amazon-bedrock": {
				"models": {
					"anthropic.claude-test-v1:0": {
						"modalities": {"input": ["text"]},
						"limit": {"context": 200000, "output": 4096},
						"cost": {"input": 3, "output": 15}
					}
				}
			}
		}`)

		data, err := GeneratePricingRegistryFromModelsDev(source)
		Expect(err).To(BeNil())

		registry, err := loadPricingRegistry(data)
		Expect(err).To(BeNil())
		Expect(registry[api.LLMBackendOpenAI]).To(HaveLen(2))
		Expect(registry[api.LLMBackendOpenAI]["gpt-test"].SupportsImages).To(BeTrue())
		Expect(registry[api.LLMBackendOpenAI]["gpt-test"].SupportsPromptCache).To(BeTrue())
		Expect(registry[api.LLMBackendOpenAI]["gpt-test"].ContextPriceTiers).To(HaveLen(1))
		Expect(registry[api.LLMBackendGemini]).To(HaveKey("gemini-test"))
		Expect(registry[api.LLMBackendBedrock]).To(HaveKey("anthropic.claude-test-v1:0"))
	})

	ginkgo.It("does not serialize unknown cost as zero", func() {
		genInfo := GenerationInfo{
			InputTokens:          10,
			OutputTokens:         5,
			CostCalculationError: ptr("missing model"),
		}

		data, err := json.Marshal(genInfo)
		Expect(err).To(BeNil())

		var result map[string]any
		Expect(json.Unmarshal(data, &result)).To(Succeed())
		Expect(result).ToNot(HaveKey("cost"))
	})

	ginkgo.It("serializes calculated zero cost", func() {
		genInfo := GenerationInfo{
			Cost: ptr(0.0),
		}

		data, err := json.Marshal(genInfo)
		Expect(err).To(BeNil())

		var result map[string]any
		Expect(json.Unmarshal(data, &result)).To(Succeed())
		Expect(result).To(HaveKeyWithValue("cost", BeNumerically("==", 0)))
	})
})

func ptr[T any](v T) *T {
	return &v
}
