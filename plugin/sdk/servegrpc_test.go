package sdk

import (
	"context"
	"net"
	"time"

	"github.com/flanksource/incident-commander/plugin/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var _ = ginkgo.Describe("ServeGRPC", func() {
	ginkgo.It("serves the plugin service over a plain gRPC listener", func() {
		grpcServer, httpServer, err := newGRPCServer(httpTestPlugin{})
		Expect(err).NotTo(HaveOccurred())
		defer httpServer.Close()

		lis, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		go func() { _ = grpcServer.Serve(lis) }()
		defer grpcServer.Stop()

		conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		client := api.NewPluginServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		manifest, err := client.RegisterPlugin(ctx, &api.RegisterRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(manifest.Name).To(Equal("http-test"))

		health, err := client.Health(ctx, &api.Empty{})
		Expect(err).NotTo(HaveOccurred())
		Expect(health.Ok).To(BeTrue())
	})
})
