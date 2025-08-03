package llm

import (
	"fmt"
	"testing"

	"github.com/flanksource/incident-commander/api"
	"github.com/onsi/gomega"
)

func TestCalculateCost(t *testing.T) {
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
			model:        "claude-3-5-sonnet-latest",
			inputTokens:  15000,
			outputTokens: 20000,
			expectedCost: 0.3450,
		},
		{
			provider:     api.LLMBackendBedrock,
			model:        "anthropic.claude-v2",
			inputTokens:  1000,
			outputTokens: 1000,
			expectedCost: 0, // no cost logic yet, just included for compilation
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s %s", test.provider, test.model), func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			cost, err := CalculateCost(test.provider, test.model, GenerationInfo{
				InputTokens:  test.inputTokens,
				OutputTokens: test.outputTokens,
			})
			g.Expect(err).To(gomega.BeNil())
			g.Expect(cost).To(gomega.BeNumerically("~", test.expectedCost, 0.0001), "expected to be equal to 4 decimal places")
		})
	}
}
