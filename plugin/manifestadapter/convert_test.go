package manifestadapter

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/flanksource/incident-commander/plugin/api"
)

var _ = ginkgo.Describe("ManifestToService", func() {
	ginkgo.It("returns an empty service for a nil manifest", func() {
		service := ManifestToService(nil)
		Expect(service.Name).To(BeEmpty())
		Expect(service.Operations).To(BeEmpty())
	})

	ginkgo.It("translates the client-visible plugin fields", func() {
		manifest := &api.PluginManifest{
			Name:        "golang",
			Version:     "v0.1.0",
			Description: "Go diagnostics",
			Operations: []*api.OperationDef{
				{Name: "pods-list", Description: "list pods", Scope: "config"},
				{Name: "session-create", Description: "create session", Scope: "config"},
			},
		}
		service := ManifestToService(manifest)
		Expect(service.Name).To(Equal("golang"))
		Expect(service.Version).To(Equal("v0.1.0"))
		Expect(service.Description).To(Equal("Go diagnostics"))
		Expect(service.Operations).To(HaveLen(2))
		Expect(service.Operations[0].Name).To(Equal("pods-list"))
		Expect(service.Operations[0].Description).To(Equal("list pods"))
		Expect(service.Operations[0].Tags).To(Equal([]string{"config"}))
	})

	ginkgo.It("yields zero parameters when params_schema is empty", func() {
		operation := operationDefToPlugin(&api.OperationDef{Name: "x"})
		Expect(operation.Parameters).To(BeEmpty())
		Expect(operation.Schema.Type).To(Equal("object"))
		Expect(operation.Schema.Properties).To(BeEmpty())
	})

	ginkgo.It("converts a JSON-Schema-shaped params_schema", func() {
		schema, err := structpb.NewStruct(map[string]any{
			"type":     "object",
			"required": []any{"podName"},
			"properties": map[string]any{
				"podName": map[string]any{
					"type":        "string",
					"description": "Name of the pod to target",
				},
				"port": map[string]any{
					"type":    "integer",
					"default": float64(6060),
				},
				"mode": map[string]any{
					"type": "string",
					"enum": []any{"cpu", "heap", "trace"},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		operation := operationDefToPlugin(&api.OperationDef{
			Name:         "profile-collect",
			Description:  "collect a profile",
			ParamsSchema: schema,
			Scope:        "config",
		})

		Expect(operation.Schema.Required).To(ContainElement("podName"))
		Expect(operation.Schema.Properties).To(HaveKey("podName"))
		Expect(operation.Schema.Properties).To(HaveKey("port"))
		Expect(operation.Schema.Properties["port"].Type).To(Equal("integer"))
		Expect(operation.Schema.Properties["mode"].Enum).To(ConsistOf("cpu", "heap", "trace"))

		parameters := map[string]bool{}
		parameterNames := make([]string, 0, len(operation.Parameters))
		for _, parameter := range operation.Parameters {
			parameters[parameter.Name] = parameter.Required
			parameterNames = append(parameterNames, parameter.Name)
		}
		Expect(parameterNames).To(Equal([]string{"mode", "podName", "port"}))
		Expect(parameters).To(HaveKeyWithValue("podName", true))
		Expect(parameters).To(HaveKey("port"))
		Expect(parameters).To(HaveKey("mode"))
	})
})
