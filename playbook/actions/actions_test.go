package actions

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/secret"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

func TestSensitiveParametersLeakage(t *testing.T) {
	apiKey := secret.Sensitive("my_secret")
	templateEnv := TemplateEnv{
		Params: map[string]any{
			"apikey": apiKey,
		},
	}

	type testCase struct {
		name     string
		template string
	}

	t.Run("go template", func(t *testing.T) {
		gotemplateTestCases := []testCase{
			{name: "non existing func", template: "{{.params.apikey}} {{nonExistingFunc .}}"},
			{name: "panic in template", template: `{{panic "error"}}`},
			{name: "index out of range", template: `{{index .params.apikey 999}}`},
			{name: "syntax error - missing closing brace", template: "{{.params.apikey {{.other}}"},
			{name: "syntax error - unexpected end", template: "{{if .params.apikey}}{{end"},
			{name: "type mismatch - expecting string", template: "{{.params.apikey | toInt}}"},
			{name: "type mismatch - invalid pipeline", template: "{{.params.apikey | nonexistentFunc}}"},
			{name: "error in nested template", template: "{{template \"nonexistent\" .params.apikey}}"},
			{name: "division by zero", template: "{{.params.apikey | div 0}}"},
			{name: "invalid pipeline order", template: "{{nonexistentFunc . | .params.apikey}}"},
			{name: "invalid conditional", template: "{{if .params.apikey ==}}"},
		}

		t.Run("gomplate", func(t *testing.T) {
			for _, td := range gotemplateTestCases {
				t.Run(td.name, func(t *testing.T) {
					g := NewWithT(t)

					env := templateEnv.AsMap(context.New())
					_, err := gomplate.RunTemplate(env, gomplate.Template{Template: td.template})
					g.Expect(err).ToNot(BeNil())
					g.Expect(err.Error()).To(Not(ContainSubstring(apiKey.PlainText())))
				})
			}
		})

		t.Run("struct templater", func(t *testing.T) {
			ctx := context.New()
			type structToTemplate struct {
				Field string `json:"field" template:"true"`
			}

			for _, td := range gotemplateTestCases {
				t.Run(td.name, func(t *testing.T) {
					g := NewWithT(t)

					templater := ctx.NewStructTemplater(templateEnv.AsMap(ctx), "template", nil)
					data := structToTemplate{Field: td.template}
					err := templater.Walk(&data)
					g.Expect(err).ToNot(BeNil())
					g.Expect(err.Error()).To(Not(ContainSubstring(apiKey.PlainText())))
				})
			}
		})
	})

	t.Run("cel expression", func(t *testing.T) {
		celTestCases := []testCase{
			{name: "non existing func", template: "params.apikey && nonExisting"},
			{name: "non existing var", template: "params.apikey  && nonExisting"},
			{name: "syntax error - consecutive operators", template: "params.apikey && && anotherVar"},
			{name: "syntax error - missing operand", template: "params.apikey && "},
			{name: "type mismatch - adding string and number", template: "params.apikey + 10"},
			{name: "type mismatch - incorrect function argument", template: "format(\"%d\", params.apikey)"},
			{name: "division by zero", template: "10 / 0"},
			{name: "unknown operator", template: "params.apikey ** 2"},
			{name: "invalid function usage", template: "printf(\"%s\", params.apikey, extraArg)"},
			{name: "invalid variable access", template: "params.apikey.subfield"},
			{name: "extra operand", template: "params.apikey && true false"},
		}

		for _, td := range celTestCases {
			t.Run(td.name, func(t *testing.T) {
				g := NewWithT(t)
				env := templateEnv.AsMap(context.New())
				_, err := gomplate.RunTemplate(env, gomplate.Template{Expression: td.template})
				g.Expect(err).ToNot(BeNil())
				g.Expect(err.Error()).To(Not(ContainSubstring(apiKey.PlainText())))
			})
		}
	})

}

func TestTemplateEnv(t *testing.T) {
	templateEnv := TemplateEnv{
		Config: &models.ConfigItem{
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

		env := templateEnv.AsMap(context.New())
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

		env := templateEnv.AsMap(context.New())
		g.Expect(env).To(HaveKey("component"))
		g.Expect(env).To(HaveKey("config"))
		g.Expect(env).To(HaveKey("check"))

		g.Expect(env["check"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["component"]).To(HaveKeyWithValue("name", ""))
		g.Expect(env["config"]).To(HaveKeyWithValue("name", "podinfo"))
	})

	t.Run("resource field aliases", func(t *testing.T) {
		g := NewWithT(t)

		env := templateEnv.AsMap(context.New())
		g.Expect(env).To(HaveKeyWithValue("name", "podinfo"))
		g.Expect(env).To(HaveKeyWithValue("health", string(models.HealthHealthy)))
		g.Expect(env).To(HaveKeyWithValue("status", "Running"))

		g.Expect(env).To(HaveKey("labels"))
		g.Expect(env).To(HaveKey("tags"))

		g.Expect(env["labels"]).To(HaveKeyWithValue("app", "podinfo"))
		g.Expect(env["tags"]).To(HaveKeyWithValue("namespace", "default"))
	})
}
