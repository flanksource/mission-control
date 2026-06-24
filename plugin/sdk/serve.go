package sdk

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/flanksource/incident-commander/plugin/api"
)

// Option configures Serve.
type Option func(*serveOptions)

type serveOptions struct {
	staticAssets fs.FS

	// httpBindHost is the interface the static-asset/operations HTTP server binds
	// to. Empty means loopback (go-plugin subprocess mode, reached only by the
	// local host). ServeGRPC sets it to all-interfaces so a remote host can reach
	// the plugin's UI and HTTP operations.
	httpBindHost string

	// tlsCertFile/tlsKeyFile, when set, make ServeGRPC serve the plugin's gRPC
	// server over TLS so the host can dial it securely. Empty means plaintext.
	// The same certificate is presented as the plugin's client certificate when
	// it dials the host back-channel (mTLS).
	tlsCertFile string
	tlsKeyFile  string

	// tlsClientCAFile, when set, makes ServeGRPC require and verify the host's
	// client certificate against this CA bundle (mTLS).
	tlsClientCAFile string
}

// WithStaticAssets mounts the given filesystem under the plugin HTTP server's
// reserved /__mc/ui/ path. Plugin authors typically pass an embed.FS containing
// their vite-built dist/ directory:
//
//	//go:embed ui/dist
//	var uiAssets embed.FS
//
//	sdk.Serve(&MyPlugin{}, sdk.WithStaticAssets(uiAssets))
func WithStaticAssets(assets fs.FS) Option {
	return func(o *serveOptions) { o.staticAssets = assets }
}

// WithServerTLS makes ServeGRPC serve the plugin's gRPC server over TLS using
// the given certificate and key files. The host must trust the certificate
// (e.g. via spec.caCert). The same certificate is presented as the plugin's
// client certificate when it dials the host back-channel. It has no effect on
// the go-plugin subprocess Serve.
func WithServerTLS(certFile, keyFile string) Option {
	return func(o *serveOptions) {
		o.tlsCertFile = certFile
		o.tlsKeyFile = keyFile
	}
}

// WithServerClientCA enables mutual TLS on ServeGRPC: the host's client
// certificate is required and verified against the given CA bundle. Requires
// WithServerTLS.
func WithServerClientCA(caFile string) Option {
	return func(o *serveOptions) { o.tlsClientCAFile = caFile }
}

// shutdownCh signals the main goroutine that the host called Shutdown.
var (
	shutdownOnce sync.Once
	shutdownCh   = make(chan struct{})
)

func shutdownSignal() {
	shutdownOnce.Do(func() { close(shutdownCh) })
}

// parentPollInterval is how often the plugin checks whether its host is still
// alive.
const parentPollInterval = time.Second

// watchParentDeath calls onDeath the first time the parent pid changes from
// original. The host launches the plugin as a direct child, so a changed ppid
// means the host process is gone — it died abruptly (crash/SIGKILL) before it
// could send the graceful Shutdown RPC. go-plugin offers no protection against
// this, so without it an abruptly-killed host leaves orphaned plugin processes
// holding their ports. Polling getppid works identically on Linux and macOS,
// where there is no kernel mechanism (Pdeathsig is Linux-only) to tear a child
// down with its parent.
func watchParentDeath(original int, interval time.Duration, getppid func() int, onDeath func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if getppid() != original {
			onDeath()
			return
		}
	}
}

// Serve is the entry point for plugin binaries. It:
//
//  1. validates the magic-cookie env var (exits 1 if not set);
//  2. binds an HTTP listener on 127.0.0.1:0 to serve static UI assets under
//     /__mc/ui/ and manifest-declared HTTP operations under /__mc/operations/;
//  3. starts the goplugin gRPC server (which prints its own handshake line
//     to stdout — go-plugin's host parses it);
//  4. blocks until either Shutdown is called or SIGTERM is received.
//
// The HTTP port is reported to the host via the manifest's ui_port field.
func Serve(impl Plugin, opts ...Option) {
	cfg := &serveOptions{}
	for _, o := range opts {
		o(cfg)
	}

	// Print the plugin banner to stderr before the gRPC handshake. go-plugin
	// pipes the plugin's stderr into the host logger so this surfaces in the
	// host process as `plugin <name>: <line>`.
	if m := impl.Manifest(); m != nil {
		fmt.Fprintf(os.Stderr, "plugin %s loading: version=%s\n", m.Name, m.Version)
	}

	srv := newPluginServer(impl, 0)
	uiPort, httpServer, err := startHTTPServer(cfg, srv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plugin sdk: %v\n", err)
		os.Exit(1)
	}
	srv.uiPort = uiPort

	go watchParentDeath(os.Getppid(), parentPollInterval, os.Getppid, func() {
		fmt.Fprintln(os.Stderr, "plugin: host process exited; shutting down")
		os.Exit(1)
	})

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]goplugin.Plugin{
			api.PluginName: &grpcAdapter{srv: srv},
		},
		GRPCServer: api.GRPCServerFactory,
	})

	// goplugin.Serve blocks until the host disconnects. Try to drain the
	// HTTP server cleanly.
	_ = httpServer.Close()
}

// ServeGRPC runs the plugin as a standalone gRPC server listening on addr
// (e.g. ":9000"), instead of as a go-plugin subprocess started by the host.
// There is no go-plugin handshake: the server simply binds the PluginService
// on a plain TCP listener and blocks until the process is signalled or the
// host calls the Shutdown RPC. The static-asset HTTP server is started exactly
// as it is under Serve.
func ServeGRPC(impl Plugin, addr string, opts ...Option) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind grpc listener: %w", err)
	}

	if m := impl.Manifest(); m != nil {
		fmt.Fprintf(os.Stderr, "plugin %s serving on %s: version=%s\n", m.Name, lis.Addr(), m.Version)
	}

	grpcServer, httpServer, err := newGRPCServer(impl, opts...)
	if err != nil {
		_ = lis.Close()
		return err
	}
	defer httpServer.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
		case <-shutdownCh:
		}
		grpcServer.GracefulStop()
	}()

	return grpcServer.Serve(lis)
}

// newGRPCServer builds the standalone gRPC server and its companion static
// HTTP server from a plugin. It mirrors Serve's wiring (newPluginServer +
// startHTTPServer + the api gRPC server factory) so both entry points dispatch
// operations identically.
func newGRPCServer(impl Plugin, opts ...Option) (*grpc.Server, *http.Server, error) {
	cfg := &serveOptions{}
	for _, o := range opts {
		o(cfg)
	}
	// A standalone server is reached over the network, so its UI/operations HTTP
	// server must bind all interfaces rather than loopback.
	if cfg.httpBindHost == "" {
		cfg.httpBindHost = "0.0.0.0"
	}

	var serverOpts []grpc.ServerOption
	// A client CA alone (mTLS) still requires the server's own certificate, so
	// entering the TLS branch on any TLS option ensures a partial config is
	// rejected by serverTLSConfig rather than silently starting in plaintext.
	if cfg.tlsCertFile != "" || cfg.tlsKeyFile != "" || cfg.tlsClientCAFile != "" {
		tlsCfg, err := serverTLSConfig(cfg.tlsCertFile, cfg.tlsKeyFile, cfg.tlsClientCAFile)
		if err != nil {
			return nil, nil, err
		}
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}

	srv := newPluginServer(impl, 0)
	// The plugin presents its own certificate as a client certificate when it
	// dials the host back-channel (mTLS).
	srv.tlsCertFile = cfg.tlsCertFile
	srv.tlsKeyFile = cfg.tlsKeyFile
	uiPort, httpServer, err := startHTTPServer(cfg, srv)
	if err != nil {
		return nil, nil, err
	}
	srv.uiPort = uiPort

	grpcServer := api.GRPCServerFactory(serverOpts)
	api.RegisterPluginServiceServer(grpcServer, srv)
	return grpcServer, httpServer, nil
}

// serverTLSConfig builds the plugin gRPC server's TLS config. When clientCAFile
// is set it requires and verifies the host's client certificate (mTLS).
func serverTLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("plugin server TLS requires both a certificate and key (--serve-tls-cert/--serve-tls-key)")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load plugin server TLS: %w", err)
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if clientCAFile != "" {
		pem, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read plugin server client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse plugin server client CA")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

func startHTTPServer(cfg *serveOptions, srv *pluginServer) (uint32, *http.Server, error) {
	mux := http.NewServeMux()
	mux.Handle("/__mc/operations/", srv.httpOperationsHandler())

	staticHandler := http.Handler(http.NotFoundHandler())
	if cfg.staticAssets != nil {
		staticHandler = http.FileServer(http.FS(cfg.staticAssets))
	}
	mux.Handle("/__mc/ui/", http.StripPrefix("/__mc/ui", staticHandler))

	bindHost := cfg.httpBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(bindHost, "0"))
	if err != nil {
		return 0, nil, fmt.Errorf("bind http listener: %w", err)
	}

	port := uint32(listener.Addr().(*net.TCPAddr).Port)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !strings.Contains(err.Error(), "Server closed") {
			fmt.Fprintf(os.Stderr, "plugin sdk: http server: %v\n", err)
		}
	}()

	return port, server, nil
}
