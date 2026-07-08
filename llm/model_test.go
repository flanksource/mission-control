package llm

import (
	genkitai "github.com/firebase/genkit/go/ai"
	genkitapi "github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("initGenkit", func() {
	for _, model := range []string{"gpt-5.1", "openai/gpt-5.1"} {
		ginkgo.It("defines OpenAI model "+model+" with native constrained output support", func() {
			g, modelName, err := initGenkit(Config{
				AIActionClient: v1.AIActionClient{
					Backend: api.LLMBackendOpenAI,
					Model:   model,
				},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(modelName).To(Equal("openai/gpt-5.1"))

			resolved := genkit.LookupModel(g, modelName)
			Expect(resolved).ToNot(BeNil())

			action, ok := resolved.(genkitapi.Action)
			Expect(ok).To(BeTrue())

			modelMetadata, ok := action.Desc().Metadata["model"].(map[string]any)
			Expect(ok).To(BeTrue())
			supports, ok := modelMetadata["supports"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(supports).To(HaveKeyWithValue("constrained", genkitai.ConstrainedSupportAll))
		})
	}
})
