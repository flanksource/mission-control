package machinery

import (
	"context"
	"net/http"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
)

type uiPortRuntime struct {
	fakeRuntime
	port uint32
}

func (r *uiPortRuntime) UIPort() uint32 { return r.port }

var _ = ginkgo.Describe("HTTPTarget", func() {
	ctx := dutyContext.NewContext(context.Background())

	register := func(address string, runtime plugin.Runtime) uuid.UUID {
		id := uuid.New()
		_, err := plugin.DefaultRegistry.Upsert(id, "default", "remote-"+id.String()[:8], v1.PluginSpec{Address: address})
		Expect(err).NotTo(HaveOccurred())
		Expect(plugin.DefaultRegistry.SetRuntime(id, runtime)).To(Succeed())
		ginkgo.DeferCleanup(func() { plugin.DefaultRegistry.Remove(id) })
		return id
	}

	ginkgo.It("uses spec.address when the plugin reports ui_port 0", func() {
		id := register("logs.mc.svc.cluster.local:9000", &uiPortRuntime{})
		target, transport, err := HTTPTarget(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(target.String()).To(Equal("http://logs.mc.svc.cluster.local:9000"))
		Expect(transport).To(BeNil())
	})

	ginkgo.It("uses https and the plugin's TLS transport when spec.caCert is set", func() {
		id := register("logs.mc.svc.cluster.local:9000", &remoteRuntime{transport: http.DefaultTransport})
		target, transport, err := HTTPTarget(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(target.String()).To(Equal("https://logs.mc.svc.cluster.local:9000"))
		Expect(transport).To(BeIdenticalTo(http.DefaultTransport))
	})

	ginkgo.It("uses the separate UI port that older plugins report", func() {
		id := register("logs.mc.svc.cluster.local:9000", &uiPortRuntime{port: 41237})
		target, transport, err := HTTPTarget(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(target.String()).To(Equal("http://logs.mc.svc.cluster.local:41237"))
		Expect(transport).To(BeNil())
	})

	ginkgo.It("rejects a spec.address that isn't host:port", func() {
		id := register("dns:///logs.mc.svc.cluster.local:9000", &uiPortRuntime{})
		_, _, err := HTTPTarget(ctx, id)
		Expect(err).To(MatchError(ContainSubstring("must be host:port")))
	})
})
