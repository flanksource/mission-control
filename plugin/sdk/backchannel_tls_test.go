package sdk

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/incident-commander/plugin/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// testCA issues leaf certificates for the mTLS back-channel test.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA() *testCA {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	cert, err := x509.ParseCertificate(der)
	Expect(err).NotTo(HaveOccurred())
	return &testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (ca *testCA) issue(cn string, ips []net.IP) (certPEM, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	Expect(err).NotTo(HaveOccurred())
	keyDER, err := x509.MarshalECPrivateKey(key)
	Expect(err).NotTo(HaveOccurred())
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

var _ = ginkgo.Describe("standalone back-channel over mTLS", func() {
	ginkgo.It("dials the host over mutual TLS so InvokeCtx.Host works", func() {
		ca := newTestCA()
		serverCertPEM, serverKeyPEM := ca.issue("host", []net.IP{net.ParseIP("127.0.0.1")})
		clientCertPEM, clientKeyPEM := ca.issue("plugin", nil)

		// Host HostService requiring and verifying the plugin's client certificate.
		serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
		Expect(err).NotTo(HaveOccurred())
		pool := x509.NewCertPool()
		Expect(pool.AppendCertsFromPEM(ca.pem)).To(BeTrue())

		host := &fakeHostService{}
		hostLis, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		hostServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientCAs:    pool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		})))
		api.RegisterHostServiceServer(hostServer, host)
		go func() { _ = hostServer.Serve(hostLis) }()
		defer hostServer.Stop()

		// Plugin's client certificate on disk, as ServeGRPC would have it.
		dir := ginkgo.GinkgoT().TempDir()
		certFile := filepath.Join(dir, "client.crt")
		keyFile := filepath.Join(dir, "client.key")
		Expect(os.WriteFile(certFile, clientCertPEM, 0o600)).To(Succeed())
		Expect(os.WriteFile(keyFile, clientKeyPEM, 0o600)).To(Succeed())

		var seen *api.ConfigItem
		plugin := httpTestPlugin{ops: []Operation{{
			Def: &api.OperationDef{Name: "use-host"},
			Handler: func(ctx context.Context, req InvokeCtx) (any, error) {
				item, err := req.Host.GetConfigItem(ctx, "cfg-mtls")
				seen = item
				return "ok", err
			},
		}}}

		srv := newPluginServer(plugin, 0)
		srv.tlsCertFile = certFile
		srv.tlsKeyFile = keyFile

		_, err = srv.RegisterPlugin(context.Background(), &api.RegisterRequest{
			HostGrpcAddress: hostLis.Addr().String(),
			HostGrpcTls:     true,
			HostGrpcCaCert:  string(ca.pem),
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = srv.Invoke(context.Background(), &api.InvokeRequest{Operation: "use-host"})
		Expect(err).NotTo(HaveOccurred())

		Expect(host.gotID).To(Equal("cfg-mtls"))
		Expect(seen).NotTo(BeNil())
		Expect(seen.Name).To(Equal("from-host"))
	})
})
