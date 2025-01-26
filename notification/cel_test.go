package notification

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

func TestCelVariable(t *testing.T) {
	celVariables := celVariables{
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

	t.Run("tags as root level variables", func(t *testing.T) {
		g := NewWithT(t)

		env := celVariables.AsMap(context.New())
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
			g.Expect(err).To(BeNil())
			g.Expect(result).To(Equal(tt.expected))
		}
	})

	t.Run("no null variables", func(t *testing.T) {
		g := NewWithT(t)

		env := celVariables.AsMap(context.New())
		g.Expect(env).To(HaveKey("component"))
		g.Expect(env).To(HaveKey("config"))
		g.Expect(env).To(HaveKey("check"))

		g.Expect(env["check"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["component"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["config"]).To(HaveKeyWithValue("name", "podinfo"))
	})

	t.Run("resource field aliases", func(t *testing.T) {
		g := NewWithT(t)

		env := celVariables.AsMap(context.New())
		g.Expect(env).To(HaveKeyWithValue("name", "podinfo"))
		g.Expect(env).To(HaveKeyWithValue("health", string(models.HealthHealthy)))
		g.Expect(env).To(HaveKeyWithValue("status", "Running"))

		g.Expect(env).To(HaveKey("labels"))
		g.Expect(env).To(HaveKey("tags"))

		g.Expect(env["labels"]).To(HaveKeyWithValue("app", "podinfo"))
		g.Expect(env["tags"]).To(HaveKeyWithValue("namespace", "default"))
	})
}
