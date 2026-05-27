package sdk

import (
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/flanksource/incident-commander/plugin/machinery/local"
)

// Option configures Serve.
type Option func(*serveOptions)

type serveOptions struct {
	staticAssets fs.FS
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

// shutdownCh signals the main goroutine that the host called Shutdown.
var (
	shutdownOnce sync.Once
	shutdownCh   = make(chan struct{})
)

func shutdownSignal() {
	shutdownOnce.Do(func() { close(shutdownCh) })
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
	uiPort, httpServer := startHTTPServer(cfg, srv)
	srv.uiPort = uiPort

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: local.Handshake,
		Plugins: map[string]goplugin.Plugin{
			local.PluginName: &grpcAdapter{srv: srv},
		},
		GRPCServer: local.GRPCServerFactory,
	})

	// goplugin.Serve blocks until the host disconnects. Try to drain the
	// HTTP server cleanly.
	_ = httpServer.Close()
}

func startHTTPServer(cfg *serveOptions, srv *pluginServer) (uint32, *http.Server) {
	mux := http.NewServeMux()
	mux.Handle("/__mc/operations/", srv.httpOperationsHandler())

	staticHandler := http.Handler(http.NotFoundHandler())
	if cfg.staticAssets != nil {
		staticHandler = http.FileServer(http.FS(cfg.staticAssets))
	}
	mux.Handle("/__mc/ui/", http.StripPrefix("/__mc/ui", staticHandler))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "plugin sdk: bind http listener: %v\n", err)
		os.Exit(1)
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

	return port, server
}
