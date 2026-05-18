package controller

import (
	"net/http"
	"net/url"

	v1 "github.com/flanksource/incident-commander/api/v1"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("plugin HTTP proxy", func() {
	ginkgo.It("rewrites UI traffic to the reserved static UI mount", func() {
		prefix := "/api/plugins/kubernetes-logs/ui"
		Expect(pluginUITargetPath(prefix, prefix)).To(Equal("/__mc/ui/"))
		Expect(pluginUITargetPath(prefix, prefix+"/assets/app.js")).To(Equal("/__mc/ui/assets/app.js"))
	})

	ginkgo.It("rewrites operation traffic to the reserved operation mount", func() {
		Expect(pluginOperationTargetPath("logs")).To(Equal("/__mc/operations/logs"))
	})

	ginkgo.It("allows declared HTTP operation methods", func() {
		def := &pluginpb.OperationDef{Http: []*pluginpb.HTTPBinding{{Method: http.MethodGet}}}
		Expect(operationHTTPBindingAllowed(def, http.MethodGet)).To(BeTrue())
		Expect(operationHTTPBindingAllowed(def, http.MethodPost)).To(BeFalse())
		Expect(operationHTTPBindingAllowed(&pluginpb.OperationDef{}, http.MethodGet)).To(BeFalse())
	})

	ginkgo.It("excludes config_id and sorts query values for HTTP params hashes", func() {
		left := url.Values{
			"config_id": []string{"config-a"},
			"level":     []string{"info"},
			"pod":       []string{"b", "a"},
		}
		right := url.Values{
			"config_id": []string{"config-b"},
			"pod":       []string{"a", "b"},
			"level":     []string{"info"},
		}

		Expect(httpParamsHash(http.MethodGet, left)).To(Equal(httpParamsHash(http.MethodGet, right)))
		Expect(httpParamsHash(http.MethodPost, left)).ToNot(Equal(httpParamsHash(http.MethodGet, left)))
	})

	ginkgo.It("uses only stable fields for fingerprints", func() {
		paramsHash := hashBytes([]byte(`{"pod":"api"}`))
		Expect(invocationFingerprint("plugin-id", "tail", "user-id", paramsHash)).To(Equal(invocationFingerprint("plugin-id", "tail", "user-id", paramsHash)))
		Expect(invocationFingerprint("plugin-id", "tail", "other-user", paramsHash)).ToNot(Equal(invocationFingerprint("plugin-id", "tail", "user-id", paramsHash)))
	})

	ginkgo.It("audits invocation changes using plugin spec match expressions", func() {
		Expect(pluginInvocationAudited(nil, "logs")).To(BeFalse())
		Expect(pluginInvocationAudited(&registry.Entry{}, "logs")).To(BeFalse())
		Expect(pluginInvocationAudited(&registry.Entry{Spec: v1.PluginSpec{Audit: []string{"logs"}}}, "logs")).To(BeTrue())
		Expect(pluginInvocationAudited(&registry.Entry{Spec: v1.PluginSpec{Audit: []string{"logs"}}}, "exec")).To(BeFalse())
		Expect(pluginInvocationAudited(&registry.Entry{Spec: v1.PluginSpec{Audit: []string{"*", "!debug"}}}, "exec")).To(BeTrue())
		Expect(pluginInvocationAudited(&registry.Entry{Spec: v1.PluginSpec{Audit: []string{"*", "!debug"}}}, "debug")).To(BeFalse())
	})
})
