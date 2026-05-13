package controller

import (
	"net/http"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("plugin HTTP proxy", func() {
	ginkgo.It("rewrites UI traffic to the reserved static UI mount", func() {
		prefix := "/api/plugins/kubernetes-logs/ui"
		Expect(pluginProxyTargetPath(prefix, prefix, "", false)).To(Equal("/__mc/ui/"))
		Expect(pluginProxyTargetPath(prefix, prefix+"/assets/app.js", "", false)).To(Equal("/__mc/ui/assets/app.js"))
	})

	ginkgo.It("rewrites operation traffic to the reserved operation mount", func() {
		prefix := "/api/plugins/kubernetes-logs/proxy/logs"
		Expect(pluginProxyTargetPath(prefix, prefix, "logs", true)).To(Equal("/__mc/operations/logs"))
		Expect(pluginProxyTargetPath(prefix, prefix+"/stream", "logs", true)).To(Equal("/__mc/operations/logs"))
	})

	ginkgo.It("allows declared HTTP operation methods", func() {
		def := &pluginpb.OperationDef{Http: []*pluginpb.HTTPBinding{{Method: http.MethodGet}}}
		Expect(operationHTTPBindingAllowed(def, http.MethodGet)).To(BeTrue())
		Expect(operationHTTPBindingAllowed(def, http.MethodPost)).To(BeFalse())
		Expect(operationHTTPBindingAllowed(&pluginpb.OperationDef{}, http.MethodGet)).To(BeFalse())
	})
})
