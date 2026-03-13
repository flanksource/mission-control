package llm

import (
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
			model:        "claude-3-5-sonnet-latest",
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
})
