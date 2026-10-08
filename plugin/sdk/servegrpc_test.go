package sdk

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing/fstest"
	"time"

	"github.com/flanksource/incident-commander/plugin/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var _ = ginkgo.Describe("ServeGRPC", func() {
	assets := WithStaticAssets(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("plugin ui")},
	})

	// serveOneListener starts the standalone server and checks that its single
	// listener serves the UI over HTTP/1.1 and the PluginService over gRPC.
	serveOneListener := func(scheme string, httpClient *http.Client, grpcCreds credentials.TransportCredentials, opts ...Option) {
		server, err := newStandaloneServer(httpTestPlugin{}, append(opts, assets)...)
		Expect(err).NotTo(HaveOccurred())

		lis, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		go func() {
			if server.TLSConfig != nil {
				_ = server.ServeTLS(lis, "", "")
			} else {
				_ = server.Serve(lis)
			}
		}()
		defer server.Close()

		res, err := httpClient.Get(scheme + "://" + lis.Addr().String() + "/__mc/ui/")
		Expect(err).NotTo(HaveOccurred())
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.StatusCode).To(Equal(http.StatusOK))
		Expect(res.ProtoMajor).To(Equal(1))
		Expect(string(body)).To(Equal("plugin ui"))

		conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(grpcCreds))
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		client := api.NewPluginServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		manifest, err := client.RegisterPlugin(ctx, &api.RegisterRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(manifest.Name).To(Equal("http-test"))
		Expect(manifest.UiPort).To(BeZero())

		health, err := client.Health(ctx, &api.Empty{})
		Expect(err).NotTo(HaveOccurred())
		Expect(health.Ok).To(BeTrue())
	}

	ginkgo.It("serves the UI and gRPC on one plaintext listener", func() {
		serveOneListener("http", &http.Client{Timeout: 2 * time.Second}, insecure.NewCredentials())
	})

	ginkgo.It("serves the UI and gRPC on one TLS listener", func() {
		ca := newTestCA()
		certPEM, keyPEM := ca.issue("plugin", []net.IP{net.ParseIP("127.0.0.1")})
		dir := ginkgo.GinkgoT().TempDir()
		certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
		Expect(os.WriteFile(certFile, certPEM, 0o600)).To(Succeed())
		Expect(os.WriteFile(keyFile, keyPEM, 0o600)).To(Succeed())

		roots := x509.NewCertPool()
		Expect(roots.AppendCertsFromPEM(ca.pem)).To(BeTrue())
		clientTLS := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}

		httpClient := &http.Client{
			Timeout:   2 * time.Second,
			Transport: &http.Transport{TLSClientConfig: clientTLS.Clone()},
		}
		serveOneListener("https", httpClient, credentials.NewTLS(clientTLS), WithServerTLS(certFile, keyFile))
	})
})
