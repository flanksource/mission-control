package v1

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/cel-go/cel"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

// denyRule returns the CEL rule refusing deny, from the generated CRD's validations at the given path of its schema.
func denyRule(crdFile string, path ...string) string {
	raw, err := os.ReadFile("../../config/crds/" + crdFile)
	Expect(err).ToNot(HaveOccurred())

	var crd map[string]any
	Expect(yaml.Unmarshal(raw, &crd)).To(Succeed())

	node := crd["spec"].(map[string]any)["versions"].([]any)[0].(map[string]any)["schema"].(map[string]any)["openAPIV3Schema"]
	for _, key := range path {
		node = node.(map[string]any)[key]
	}

	for _, v := range node.(map[string]any)["x-kubernetes-validations"].([]any) {
		validation := v.(map[string]any)
		if rule := validation["rule"].(string); rule == "!has(self.deny) || self.deny != true" {
			return rule
		}
	}
	ginkgo.Fail("no validation refuses deny in " + crdFile)
	return ""
}

func evalRule(rule, self string) bool {
	env, err := cel.NewEnv(cel.Variable("self", cel.MapType(cel.StringType, cel.DynType)))
	Expect(err).ToNot(HaveOccurred())
	ast, issues := env.Compile(rule)
	Expect(issues.Err()).ToNot(HaveOccurred())
	program, err := env.Program(ast)
	Expect(err).ToNot(HaveOccurred())

	var object map[string]any
	Expect(json.Unmarshal([]byte(self), &object)).To(Succeed())
	out, _, err := program.Eval(map[string]any{"self": object})
	Expect(err).ToNot(HaveOccurred())
	return out.Value() == true
}

var _ = ginkgo.Describe("deny", func() {
	crds := []struct {
		kind, file string
		path       []string
		object     string
	}{
		{"Role rule", "mission-control.flanksource.com_roles.yaml",
			[]string{"properties", "spec", "properties", "rules", "items"},
			`{"name":"run","action":"playbook:run","resource":{"scopeRef":"all"}%s}`},
		{"Permission", "mission-control.flanksource.com_permissions.yaml",
			[]string{"properties", "spec"},
			`{"actions":["playbook:run"],"subject":{"person":"a@b.com"},"object":{"playbooks":[{"name":"*"}]}%s}`},
	}

	for _, crd := range crds {
		ginkgo.It("is refused on a "+crd.kind+" by the CRD", func() {
			rule := denyRule(crd.file, crd.path...)
			with := func(field string) string { return fmt.Sprintf(crd.object, field) }

			Expect(evalRule(rule, with(`,"deny":true`))).To(BeFalse())
			Expect(evalRule(rule, with(`,"deny":false`))).To(BeTrue())
			Expect(evalRule(rule, with(``))).To(BeTrue())
		})
	}
})
