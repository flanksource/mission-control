// Checks the View CRD's CEL rules on templating variables, as the API server applies them to a View
// the controller writes back after converting it to its Go type.
package v1

import (
	"context"
	"encoding/json"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"sigs.k8s.io/yaml"
)

// viewWithVariable is a View with one templating variable, whose YAML follows.
const viewWithVariable = `
apiVersion: mission-control.flanksource.com/v1
kind: View
metadata:
  name: variables
  namespace: default
spec:
  panels:
    - name: Pods
      type: number
      query: SELECT COUNT(*) AS value FROM pods
  queries:
    pods:
      configs:
        search: type=Kubernetes::Pod
  templating:
    - `

// validateWrittenBack converts manifest to the View Go type and back, as the controller does
// before it writes the View with its finalizer, and applies the generated CRD's CEL rules to it.
func validateWrittenBack(manifest string) field.ErrorList {
	raw, err := os.ReadFile("../../config/crds/mission-control.flanksource.com_views.yaml")
	Expect(err).ToNot(HaveOccurred())
	var crd apiextensionsv1.CustomResourceDefinition
	Expect(yaml.Unmarshal(raw, &crd)).To(Succeed())

	var props apiextensions.JSONSchemaProps
	Expect(apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(crd.Spec.Versions[0].Schema.OpenAPIV3Schema, &props, nil)).To(Succeed())
	structural, err := schema.NewStructural(&props)
	Expect(err).ToNot(HaveOccurred())

	var view View
	Expect(yaml.Unmarshal([]byte(manifest), &view)).To(Succeed())
	data, err := json.Marshal(view)
	Expect(err).ToNot(HaveOccurred())
	var obj map[string]any
	Expect(json.Unmarshal(data, &obj)).To(Succeed())

	errs, _ := cel.NewValidator(structural, true, celconfig.PerCallLimit).
		Validate(context.Background(), field.NewPath("root"), structural, obj, nil, celconfig.RuntimeCELCostBudget)
	return errs
}

var _ = ginkgo.Describe("View CRD templating variables", func() {
	for _, tt := range []struct {
		name     string
		variable string
		err      string
	}{
		{name: "a text input, which takes any value", variable: "{key: org, label: Organization ID}"},
		{name: "a text input with an empty values list, which the controller writes back without it", variable: "{key: org, label: Organization ID, values: []}"},
		{name: "a fixed list of values", variable: "{key: env, label: Environment, values: [dev, prod]}"},
		{name: "values from config items", variable: "{key: namespace, label: Namespace, valueFrom: {config: {search: type=Kubernetes::Namespace}}}"},
		{
			name:     "both values and valueFrom",
			variable: "{key: namespace, label: Namespace, values: [default], valueFrom: {config: {search: type=Kubernetes::Namespace}}}",
			err:      "values and valueFrom can't both be set",
		},
	} {
		ginkgo.It(tt.name, func() {
			errs := validateWrittenBack(viewWithVariable + tt.variable)
			if tt.err == "" {
				Expect(errs).To(BeEmpty())
			} else {
				Expect(errs.ToAggregate()).To(MatchError(ContainSubstring(tt.err)))
			}
		})
	}
})
