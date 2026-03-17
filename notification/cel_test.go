package notification

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("CelVariable", func() {
	var (
		vars celVariables
		env  map[string]any
	)

	ginkgo.BeforeEach(func() {
		vars = celVariables{
			Permalink: "https://example.com",
			ConfigItem: &models.ConfigItem{
				Name:   lo.ToPtr("podinfo"),
				Health: lo.ToPtr(models.HealthHealthy),
				Status: lo.ToPtr("Running"),
				Type:   lo.ToPtr("Kubernetes::Pod"),
				Labels: &types.JSONStringMap{
					"app": "podinfo",
				},
				Tags: map[string]string{
					"namespace":   "default",
					"region":      "us-west-1",
					"environment": "prod",
				},
			},
		}
		env = vars.AsMap(context.New())
	})

	ginkgo.It("should expose tags as root level variables", func() {
		tests := []struct {
			expr     string
			expected string
		}{
			{expr: `region`, expected: "us-west-1"},
			{expr: `region == "us-west-1"`, expected: "true"},
			{expr: `region == "us-west-2"`, expected: "false"},
			{expr: `environment == "prod"`, expected: "true"},
			{expr: `permalink == "https://example.com"`, expected: "true"},
		}

		for _, tt := range tests {
			result, err := gomplate.RunTemplate(env, gomplate.Template{Expression: tt.expr})
			Expect(err).To(BeNil())
			Expect(result).To(Equal(tt.expected))
		}
	})

	ginkgo.It("should have no null variables", func() {
		Expect(env).To(HaveKey("component"))
		Expect(env).To(HaveKey("config"))
		Expect(env).To(HaveKey("check"))

		Expect(env["check"]).To(HaveKeyWithValue("name", ""))
		Expect(env["component"]).To(HaveKeyWithValue("name", ""))
		Expect(env["config"]).To(HaveKeyWithValue("name", "podinfo"))
	})

	ginkgo.It("should expose resource field aliases", func() {
		Expect(env).To(HaveKeyWithValue("name", "podinfo"))
		Expect(env).To(HaveKeyWithValue("health", string(models.HealthHealthy)))
		Expect(env).To(HaveKeyWithValue("status", "Running"))

		Expect(env).To(HaveKey("labels"))
		Expect(env).To(HaveKey("tags"))

		Expect(env["labels"]).To(HaveKeyWithValue("app", "podinfo"))
		Expect(env["tags"]).To(HaveKeyWithValue("namespace", "default"))
	})
})
