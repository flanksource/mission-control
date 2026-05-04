package sdk

import (
	"bytes"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/flanksource/incident-commander/plugin"
)

// Option configures Serve.
type Option func(*serveOptions)

type serveOptions struct {
	staticAssets fs.FS
}

// WithStaticAssets mounts the given filesystem at the root of the plugin's
// HTTP server. Plugin authors typically pass an embed.FS containing their
// vite-built dist/ directory:
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
//  2. binds an HTTP listener on 127.0.0.1:0 to serve the static assets and
//     the plugin's /api/* routes;
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

	uiPort, httpServer := startHTTPServer(impl, cfg)
	srv := newPluginServer(impl, uiPort)

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: plugin.Handshake,
		Plugins: map[string]goplugin.Plugin{
			plugin.PluginName: &grpcAdapter{srv: srv},
		},
		GRPCServer: plugin.GRPCServerFactory,
	})

	// goplugin.Serve blocks until the host disconnects. Try to drain the
	// HTTP server cleanly.
	_ = httpServer.Close()
}

func startHTTPServer(impl Plugin, cfg *serveOptions) (uint32, *http.Server) {
	mux := http.NewServeMux()

	// The host proxy strips /api/plugins/<name>/ui and forwards the
	// remainder, so the plugin sees a flat path. The plugin's HTTPHandler
	// wins on routes it claims; anything that comes back as a buffered 404
	// falls through to the static file server. Streaming responses (the
	// plugin calls Flush or Hijack) are committed immediately and never
	// fall through.
	pluginHandler := impl.HTTPHandler()
	var staticHandler http.Handler
	if cfg.staticAssets != nil {
		staticHandler = http.FileServer(http.FS(cfg.staticAssets))
	}
	mux.Handle("/", composeHandler(pluginHandler, staticHandler))

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

// composeHandler runs the plugin handler first; on a buffered 404 with no
// streaming, it replays the request against the static asset server. If
// either handler is nil, the other handles the request alone.
func composeHandler(plugin, static http.Handler) http.Handler {
	if plugin == nil {
		if static == nil {
			return http.HandlerFunc(http.NotFound)
		}
		return static
	}
	if static == nil {
		return plugin
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := &bufferedResponse{header: http.Header{}, target: w}
		plugin.ServeHTTP(buf, r)
		if buf.committed {
			return
		}
		if buf.status != http.StatusNotFound && buf.status != 0 {
			buf.flushTo(w)
			return
		}
		static.ServeHTTP(w, r)
	})
}

// bufferedResponse captures a handler's output so we can decide whether to
// fall through to the static asset server. As soon as the handler signals
// streaming (Flush, Hijack) we commit and write through to the underlying
// ResponseWriter — we never fall through after that.
type bufferedResponse struct {
	header    http.Header
	body      bytes.Buffer
	status    int
	committed bool
	passthru  http.ResponseWriter
	target    http.ResponseWriter
}

// Flush implements http.Flusher. Calling Flush is the plugin signalling it
// is streaming; we commit the buffered prelude and forward all subsequent
// writes (and Flush calls) to the real ResponseWriter.
func (b *bufferedResponse) Flush() {
	if !b.committed {
		b.commit(b.target)
	}
	if f, ok := b.passthru.(http.Flusher); ok {
		f.Flush()
	}
}

func (b *bufferedResponse) Header() http.Header {
	if b.committed && b.passthru != nil {
		return b.passthru.Header()
	}
	return b.header
}

func (b *bufferedResponse) WriteHeader(code int) {
	if b.committed {
		if b.passthru != nil {
			b.passthru.WriteHeader(code)
		}
		return
	}
	b.status = code
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.committed && b.passthru != nil {
		return b.passthru.Write(p)
	}
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

func (b *bufferedResponse) flushTo(w http.ResponseWriter) {
	for k, v := range b.header {
		w.Header()[k] = v
	}
	if b.status != 0 {
		w.WriteHeader(b.status)
	}
	_, _ = w.Write(b.body.Bytes())
}

// commit writes the buffered headers/body through and switches the writer
// into pass-through mode for the streaming tail.
func (b *bufferedResponse) commit(w http.ResponseWriter) {
	if b.committed {
		return
	}
	b.committed = true
	b.passthru = w
	for k, v := range b.header {
		w.Header()[k] = v
	}
	if b.status != 0 {
		w.WriteHeader(b.status)
	}
	_, _ = w.Write(b.body.Bytes())
	b.body.Reset()
}
