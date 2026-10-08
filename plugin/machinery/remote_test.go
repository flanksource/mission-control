package machinery

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/api"
)

type uiPortRuntime struct {
	fakeRuntime
	port uint32
}

func (r *uiPortRuntime) UIPort() uint32 { return r.port }

// countingPlugin is a PluginService that counts RegisterPlugin calls and
// reports the call number as its UI port.
type countingPlugin struct {
	api.UnimplementedPluginServiceServer
	registrations *atomic.Uint32
}

func (p countingPlugin) RegisterPlugin(context.Context, *api.RegisterRequest) (*api.PluginManifest, error) {
	n := p.registrations.Add(1)
	return &api.PluginManifest{Name: "counting", Version: "1.0.0", UiPort: n}, nil
}

// serveCountingPlugin serves a countingPlugin on addr and returns the server
// and the address it listens on.
func serveCountingPlugin(addr string, registrations *atomic.Uint32) (*grpc.Server, string) {
	lis, err := net.Listen("tcp", addr)
	Expect(err).NotTo(HaveOccurred())
	server := grpc.NewServer()
	api.RegisterPluginServiceServer(server, countingPlugin{registrations: registrations})
	go func() { _ = server.Serve(lis) }()
	return server, lis.Addr().String()
}

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

var _ = ginkgo.Describe("remote plugin", func() {
	ginkgo.It("is registered again after its server restarts", func() {
		ctx := dutyContext.NewContext(context.Background())
		registrations := &atomic.Uint32{}
		server, addr := serveCountingPlugin("127.0.0.1:0", registrations)

		id := uuid.New()
		_, err := plugin.DefaultRegistry.Upsert(id, "default", "counting", v1.PluginSpec{
			Address:         addr,
			HostGRPCAddress: "127.0.0.1:8081",
		})
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			if runtime := plugin.DefaultRegistry.PopRuntime(id); runtime != nil {
				runtime.Stop()
			}
			plugin.DefaultRegistry.Remove(id)
		})

		Expect(startRemotePlugin(ctx, plugin.DefaultRegistry.Get(id))).To(Succeed())
		Expect(registrations.Load()).To(Equal(uint32(1)))
		Expect(plugin.DefaultRegistry.Get(id).Runtime.UIPort()).To(Equal(uint32(1)))

		server.Stop()
		server, _ = serveCountingPlugin(addr, registrations)
		ginkgo.DeferCleanup(server.Stop)

		Eventually(registrations.Load, 15*time.Second, 100*time.Millisecond).Should(Equal(uint32(2)))
		Eventually(func() uint32 {
			return plugin.DefaultRegistry.Get(id).Runtime.UIPort()
		}, 2*time.Second, 50*time.Millisecond).Should(Equal(uint32(2)))
		Expect(plugin.DefaultRegistry.Get(id).Manifest.UiPort).To(Equal(uint32(2)))
	})
})
