package runner

import (
	"github.com/flanksource/gomplate/v3"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("idsFromMap", func() {
	ginkgo.It("renders resolved config IDs in a template pipeline", func() {
		env := map[string]any{
			"params": map[string]any{
				"items": []map[string]any{
					{"id": "config-a"},
					{"name": "missing-id"},
					{"id": "config-b"},
				},
			},
		}

		result, err := gomplate.RunTemplate(env, gomplate.Template{
			Template:  "{{ .params.items | idsFromMap }}",
			Functions: map[string]any{"idsFromMap": idsFromMap},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("config-a,config-b"))
	})
})
