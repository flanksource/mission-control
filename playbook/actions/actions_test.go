package actions

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/secret"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("SensitiveParametersLeakage", func() {
	apiKey := secret.Sensitive("my_secret")
	templateEnv := TemplateEnv{
		Params: map[string]any{
			"apikey": apiKey,
		},
	}

	ginkgo.It("AsMap keeps secrets as Sensitive", func() {
		env := templateEnv.AsMap(context.New())

		Expect(env).To(HaveKey("params"))
		params := env["params"].(map[string]any)
		Expect(params).To(HaveKey("apikey"))

		_, isSensitive := params["apikey"].(secret.Sensitive)
		Expect(isSensitive).To(BeTrue(), "params should contain Sensitive type, not plaintext")
	})

	ginkgo.It("AsMapForTemplating unwraps secrets", func() {
		env := templateEnv.AsMapForTemplating(context.New())

		Expect(env).To(HaveKey("params"))
		params := env["params"].(map[string]any)
		Expect(params).To(HaveKey("apikey"))

		val, isString := params["apikey"].(string)
		Expect(isString).To(BeTrue(), "params should contain plaintext string for templating")
		Expect(val).To(Equal("my_secret"))
	})

	ginkgo.It("JSON marshaling redacts secrets", func() {
		jsonStr := templateEnv.JSON(context.New())

		Expect(jsonStr).ToNot(ContainSubstring("my_secret"), "JSON should not contain plaintext secret")
		Expect(jsonStr).To(ContainSubstring("[REDACTED]"), "JSON should contain redacted placeholder")
	})

	ginkgo.It("ScrubSecrets replaces plaintext in output", func() {
		output := "Connected with key: my_secret and more my_secret"
		scrubbed := templateEnv.ScrubSecrets(output)

		Expect(scrubbed).ToNot(ContainSubstring("my_secret"))
		Expect(scrubbed).To(Equal("Connected with key: [REDACTED] and more [REDACTED]"))
	})

	ginkgo.It("ScrubSecrets handles empty string", func() {
		Expect(templateEnv.ScrubSecrets("")).To(Equal(""))
	})

	ginkgo.It("ScrubSecrets preserves non-secret content", func() {
		output := "This is normal output without secrets"
		Expect(templateEnv.ScrubSecrets(output)).To(Equal(output))
	})

	ginkgo.It("ScrubSecrets handles multiple different secrets", func() {
		multiSecretEnv := TemplateEnv{
			Params: map[string]any{
				"password": secret.Sensitive("super_secret_pwd"),
				"apiKey":   secret.Sensitive("sk-12345"),
				"normal":   "not_secret",
			},
		}
		output := "Using super_secret_pwd and sk-12345 to connect"
		scrubbed := multiSecretEnv.ScrubSecrets(output)

		Expect(scrubbed).ToNot(ContainSubstring("super_secret_pwd"))
		Expect(scrubbed).ToNot(ContainSubstring("sk-12345"))
		Expect(scrubbed).To(Equal("Using [REDACTED] and [REDACTED] to connect"))
	})

	type testCase struct {
		name     string
		template string
	}

	ginkgo.Describe("go template", func() {
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

		ginkgo.Describe("gomplate", func() {
			for _, td := range gotemplateTestCases {
				ginkgo.It(td.name, func() {
					env := templateEnv.AsMap(context.New())
					_, err := gomplate.RunTemplate(env, gomplate.Template{Template: td.template})
					Expect(err).ToNot(BeNil())
					Expect(err.Error()).To(Not(ContainSubstring(apiKey.PlainText())))
				})
			}
		})

		ginkgo.Describe("struct templater", func() {
			ctx := context.New()
			type structToTemplate struct {
				Field string `json:"field" template:"true"`
			}

			for _, td := range gotemplateTestCases {
				ginkgo.It(td.name, func() {
					templater := ctx.NewStructTemplater(templateEnv.AsMap(ctx), "template", nil)
					data := structToTemplate{Field: td.template}
					err := templater.Walk(&data)
					Expect(err).ToNot(BeNil())
					Expect(err.Error()).To(Not(ContainSubstring(apiKey.PlainText())))
				})
			}
		})
	})

	ginkgo.Describe("cel expression", func() {
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
			ginkgo.It(td.name, func() {
				env := templateEnv.AsMap(context.New())
				_, err := gomplate.RunTemplate(env, gomplate.Template{Expression: td.template})
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(Not(ContainSubstring(apiKey.PlainText())))
			})
		}
	})
})

var _ = ginkgo.Describe("TemplateEnv", func() {
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

	ginkgo.It("tags as root level variables", func() {
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
			Expect(err).To(BeNil())
			Expect(result).To(Equal(tt.expected))
		}
	})

	ginkgo.It("no null variables", func() {
		env := templateEnv.AsMap(context.New())
		Expect(env).To(HaveKey("component"))
		Expect(env).To(HaveKey("config"))
		Expect(env).To(HaveKey("check"))

		Expect(env["check"]).To(HaveKeyWithValue("name", ""))
		Expect(env["component"]).To(HaveKeyWithValue("name", ""))
		Expect(env["config"]).To(HaveKeyWithValue("name", "podinfo"))
	})

	ginkgo.It("resource field aliases", func() {
		env := templateEnv.AsMap(context.New())
		Expect(env).To(HaveKeyWithValue("name", "podinfo"))
		Expect(env).To(HaveKeyWithValue("health", string(models.HealthHealthy)))
		Expect(env).To(HaveKeyWithValue("status", "Running"))

		Expect(env).To(HaveKey("labels"))
		Expect(env).To(HaveKey("tags"))

		Expect(env["labels"]).To(HaveKeyWithValue("app", "podinfo"))
		Expect(env["tags"]).To(HaveKeyWithValue("namespace", "default"))
	})
})
