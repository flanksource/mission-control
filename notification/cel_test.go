package notification

import (
	"testing"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

func TestCelVariable(t *testing.T) {
	celVariables := celVariables{
		Permalink: "https://example.com",
		ConfigItem: &models.ConfigItem{
			Name: lo.ToPtr("dummy"),
			Tags: map[string]string{
				"region": "us-west-1",
				"env":    "prod",
			},
		},
	}

	t.Run("tags as root level variables", func(t *testing.T) {
		g := NewWithT(t)

		env := celVariables.AsMap()
		tests := []struct {
			expr     string
			expected string
		}{
			{expr: `region`, expected: "us-west-1"},
			{expr: `region == "us-west-1"`, expected: "true"},
			{expr: `region == "us-west-2"`, expected: "false"},
			{expr: `env == "prod"`, expected: "true"},
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

		env := celVariables.AsMap()
		g.Expect(env).To(HaveKey("component"))
		g.Expect(env).To(HaveKey("config"))
		g.Expect(env).To(HaveKey("check"))

		g.Expect(env["check"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["component"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["config"]).To(HaveKeyWithValue("name", "dummy"))
	})
}
