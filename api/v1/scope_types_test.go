package v1

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/cel-go/cel"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"
)

// scopeTargetSchema returns the generated CRD's schema of a Scope target and its "exactly one of" rule.
func scopeTargetSchema() (map[string]any, string) {
	raw, err := os.ReadFile("../../config/crds/mission-control.flanksource.com_scopes.yaml")
	Expect(err).ToNot(HaveOccurred())

	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema struct {
						Properties struct {
							Spec struct {
								Properties struct {
									Targets struct {
										Items map[string]any `json:"items"`
									} `json:"targets"`
								} `json:"properties"`
							} `json:"spec"`
						} `json:"properties"`
					} `json:"openAPIV3Schema"`
				} `json:"schema"`
			} `json:"versions"`
		} `json:"spec"`
	}
	Expect(yaml.Unmarshal(raw, &crd)).To(Succeed())
	Expect(crd.Spec.Versions).To(HaveLen(1))

	items := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties.Spec.Properties.Targets.Items
	validations, ok := items["x-kubernetes-validations"].([]any)
	Expect(ok).To(BeTrue())
	Expect(validations).To(HaveLen(1))
	delete(items, "x-kubernetes-validations")

	return items, validations[0].(map[string]any)["rule"].(string)
}

var _ = ginkgo.Describe("Scope CRD", func() {
	items, rule := scopeTargetSchema()

	compiler := jsonschema.NewCompiler()
	Expect(compiler.AddResource("target.json", items)).To(Succeed())
	schema, err := compiler.Compile("target.json")
	Expect(err).ToNot(HaveOccurred())

	env, err := cel.NewEnv(cel.Variable("self", cel.MapType(cel.StringType, cel.DynType)))
	Expect(err).ToNot(HaveOccurred())
	ast, issues := env.Compile(rule)
	Expect(issues.Err()).ToNot(HaveOccurred())
	program, err := env.Program(ast)
	Expect(err).ToNot(HaveOccurred())

	valid := func(raw string) bool {
		var target map[string]any
		Expect(json.Unmarshal([]byte(raw), &target)).To(Succeed())
		if schema.Validate(target) != nil {
			return false
		}
		out, _, err := program.Eval(map[string]any{"self": target})
		Expect(err).ToNot(HaveOccurred())
		return out.Value() == true
	}

	for _, kind := range WholeTypeResources {
		ginkgo.It("accepts "+kind+` with name: "*"`, func() {
			Expect(valid(fmt.Sprintf(`{"%s":{"name":"*"}}`, kind))).To(BeTrue())
		})

		ginkgo.It("rejects "+kind+" with any other name, or none", func() {
			for _, selector := range []string{`{"name":"api"}`, `{"name":"api-*"}`, `{"name":""}`, `{}`, `{"namespace":"default"}`} {
				Expect(valid(fmt.Sprintf(`{"%s":%s}`, kind, selector))).To(BeFalse(), selector)
			}
		})

		ginkgo.It("rejects "+kind+" alongside another type", func() {
			Expect(valid(fmt.Sprintf(`{"%s":{"name":"*"},"config":{"name":"*"}}`, kind))).To(BeFalse())
		})
	}

	ginkgo.It("rejects a target with no type", func() {
		Expect(valid(`{}`)).To(BeFalse())
	})
})

var _ = ginkgo.Describe("ScopeTarget", func() {
	for _, kind := range WholeTypeResources {
		ginkgo.It("converts "+kind+" to a whole-type selector", func() {
			var target ScopeTarget
			Expect(json.Unmarshal(fmt.Appendf(nil, `{"%s":{"name":"*"}}`, kind), &target)).To(Succeed())
			Expect(target.TypeCount()).To(Equal(1))

			converted, selector := target.Selector()
			Expect(converted).To(Equal(kind))
			Expect(selector.Wildcard()).To(BeTrue())
		})
	}
})
