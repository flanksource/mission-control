package actions

import (
	"testing"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

func TestCelVariable(t *testing.T) {
	env := TemplateEnv{
		Config: &models.ConfigItem{
			Name: lo.ToPtr("podinfo"),
			Type: lo.ToPtr("Kubernetes::Pod"),
			Tags: map[string]string{
				"namespace":   "default",
				"region":      "us-west-1",
				"environment": "prod",
			},
		},
	}

	t.Run("tags as root level variables", func(t *testing.T) {
		g := NewWithT(t)

		env := env.AsMap()
		tests := []struct {
			expr     string
			expected string
		}{
			{expr: `region`, expected: "us-west-1"},
			{expr: `region == "us-west-1"`, expected: "true"},
			{expr: `region == "us-west-2"`, expected: "false"},
			{expr: `config.tags.region`, expected: "us-west-1"},
			{expr: `environment == "prod"`, expected: "true"},
		}

		for _, tt := range tests {
			result, err := gomplate.RunTemplate(env, gomplate.Template{Expression: tt.expr})
			g.Expect(err).To(BeNil())
			g.Expect(result).To(Equal(tt.expected))
		}
	})

	t.Run("no null variables", func(t *testing.T) {
		g := NewWithT(t)

		env := env.AsMap()
		g.Expect(env).To(HaveKey("component"))
		g.Expect(env).To(HaveKey("config"))
		g.Expect(env).To(HaveKey("check"))

		g.Expect(env["check"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["component"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["config"]).To(HaveKeyWithValue("name", "podinfo"))
	})
}
