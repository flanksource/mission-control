package manifestcache

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/flanksource/incident-commander/plugin/api"
)

var _ = ginkgo.Describe("ManifestToService", func() {
	ginkgo.It("returns an empty service for a nil manifest", func() {
		svc := ManifestToService(nil)
		Expect(svc.Name).To(BeEmpty())
		Expect(svc.Operations).To(BeEmpty())
	})

	ginkgo.It("translates name, version, description, and operation list", func() {
		m := &api.PluginManifest{
			Name:        "golang",
			Version:     "v0.1.0",
			Description: "Go diagnostics",
			Operations: []*api.OperationDef{
				{Name: "pods-list", Description: "list pods", Scope: "config"},
				{Name: "session-create", Description: "create session", Scope: "config"},
			},
		}
		svc := ManifestToService(m)
		Expect(svc.Name).To(Equal("golang"))
		Expect(svc.Version).To(Equal("v0.1.0"))
		Expect(svc.Description).To(Equal("Go diagnostics"))
		Expect(svc.Operations).To(HaveLen(2))
		Expect(svc.Operations[0].Name).To(Equal("pods-list"))
		Expect(svc.Operations[0].Description).To(Equal("list pods"))
		Expect(svc.Operations[0].Tags).To(Equal([]string{"config"}))
	})

	ginkgo.It("yields zero parameters when params_schema is empty", func() {
		op := operationDefToRPC(&api.OperationDef{Name: "x"})
		Expect(op.Parameters).To(BeEmpty())
		Expect(op.Schema.Type).To(Equal("object"))
		Expect(op.Schema.Properties).To(BeEmpty())
	})

	ginkgo.It("walks a JSON-Schema-shaped params_schema into RPCParameters", func() {
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

		op := operationDefToRPC(&api.OperationDef{
			Name:         "profile-collect",
			Description:  "collect a profile",
			ParamsSchema: schema,
			Scope:        "config",
		})

		Expect(op.Schema.Required).To(ContainElement("podName"))
		Expect(op.Schema.Properties).To(HaveKey("podName"))
		Expect(op.Schema.Properties).To(HaveKey("port"))
		Expect(op.Schema.Properties["port"].Type).To(Equal("integer"))
		Expect(op.Schema.Properties["mode"].Enum).To(ConsistOf("cpu", "heap", "trace"))

		paramByName := map[string]bool{}
		var podNameParam *struct{ Required bool }
		for _, p := range op.Parameters {
			paramByName[p.Name] = true
			if p.Name == "podName" {
				podNameParam = &struct{ Required bool }{Required: p.Required}
			}
		}
		Expect(paramByName).To(HaveKey("podName"))
		Expect(paramByName).To(HaveKey("port"))
		Expect(paramByName).To(HaveKey("mode"))
		Expect(podNameParam).NotTo(BeNil())
		Expect(podNameParam.Required).To(BeTrue())
	})
})
