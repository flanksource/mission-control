package sdk

import (
	"context"
	"net"

	"github.com/flanksource/incident-commander/plugin/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
)

// fakeHostService is a minimal HostService that records GetConfigItem calls so
// the test can assert the standalone plugin dialed back to it.
type fakeHostService struct {
	api.UnimplementedHostServiceServer
	gotID string
}

func (f *fakeHostService) GetConfigItem(_ context.Context, req *api.GetConfigItemRequest) (*api.ConfigItem, error) {
	f.gotID = req.Id
	return &api.ConfigItem{Id: req.Id, Name: "from-host"}, nil
}

var _ = ginkgo.Describe("standalone back-channel", func() {
	ginkgo.It("dials host_grpc_address so InvokeCtx.Host reaches the host", func() {
		host := &fakeHostService{}
		hostLis, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		hostServer := grpc.NewServer()
		api.RegisterHostServiceServer(hostServer, host)
		go func() { _ = hostServer.Serve(hostLis) }()
		defer hostServer.Stop()

		var seen *api.ConfigItem
		plugin := httpTestPlugin{ops: []Operation{{
			Def: &api.OperationDef{Name: "use-host"},
			Handler: func(ctx context.Context, req InvokeCtx) (any, error) {
				item, err := req.Host.GetConfigItem(ctx, "cfg-123")
				seen = item
				return "ok", err
			},
		}}}

		srv := newPluginServer(plugin, 0)

		_, err = srv.RegisterPlugin(context.Background(), &api.RegisterRequest{
			HostGrpcAddress: hostLis.Addr().String(),
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = srv.Invoke(context.Background(), &api.InvokeRequest{Operation: "use-host"})
		Expect(err).NotTo(HaveOccurred())

		Expect(host.gotID).To(Equal("cfg-123"))
		Expect(seen).NotTo(BeNil())
		Expect(seen.Name).To(Equal("from-host"))
	})
})
