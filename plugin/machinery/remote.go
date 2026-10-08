// Remote plugin runtime: dials a standalone plugin's gRPC server (spec.address)
// instead of supervising a local binary, and routes operation invocations to it.
package machinery

import (
	gocontext "context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	commanderAPI "github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
)

const (
	remoteRegisterTimeout = 30 * time.Second

	// remoteReregisterRetry is how long to wait before retrying a failed
	// re-registration while the plugin stays reachable.
	remoteReregisterRetry = 10 * time.Second
)

// remoteRuntime is the host-side handle for a plugin reachable over the network.
// It satisfies plugin.Runtime by forwarding calls to the plugin's gRPC server.
type remoteRuntime struct {
	id         uuid.UUID
	name       string
	conn       *grpc.ClientConn
	service    pluginAPI.PluginServiceClient
	uiPort     atomic.Uint32
	transport  http.RoundTripper
	register   func(gocontext.Context) (*pluginAPI.PluginManifest, error)
	ctx        gocontext.Context
	cancel     gocontext.CancelFunc
	connEvents chan struct{}
	generation atomic.Uint64
	registered atomic.Uint64
}

func (r *remoteRuntime) Invoke(ctx gocontext.Context, req *pluginAPI.InvokeRequest) (*pluginAPI.InvokeResponse, error) {
	return r.service.Invoke(ctx, req)
}

func (r *remoteRuntime) UIPort() uint32 { return r.uiPort.Load() }

func (r *remoteRuntime) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.conn != nil {
		_ = r.conn.Close()
	}
}

func (r *remoteRuntime) TagRPC(ctx gocontext.Context, _ *stats.RPCTagInfo) gocontext.Context {
	return ctx
}

func (r *remoteRuntime) HandleRPC(gocontext.Context, stats.RPCStats) {}

func (r *remoteRuntime) TagConn(ctx gocontext.Context, _ *stats.ConnTagInfo) gocontext.Context {
	return ctx
}

// HandleConn records transport changes even when connectivity leaves and
// returns to Ready before the registration worker can observe it.
func (r *remoteRuntime) HandleConn(_ gocontext.Context, event stats.ConnStats) {
	if _, ok := event.(*stats.ConnBegin); ok {
		r.generation.Add(1)
	}
	select {
	case r.connEvents <- struct{}{}:
	default:
	}
}

// startRemotePlugin dials the plugin's gRPC server, completes RegisterPlugin
// (handing the plugin the host's HostService address so its callbacks work),
// and installs the runtime in the registry.
func startRemotePlugin(ctx dutyContext.Context, entry *plugin.Entry) error {
	tlsCfg, err := pluginTLSConfig(entry.Spec)
	if err != nil {
		return fmt.Errorf("plugin %s: %w", entry.Name, err)
	}
	dialCreds := insecure.NewCredentials()
	var transport http.RoundTripper
	if tlsCfg != nil {
		dialCreds = credentials.NewTLS(tlsCfg)
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = tlsCfg.Clone()
		transport = t
	}

	runtime := &remoteRuntime{
		id:         entry.ID,
		name:       entry.Name,
		transport:  transport,
		connEvents: make(chan struct{}, 1),
	}
	runtime.ctx, runtime.cancel = gocontext.WithCancel(gocontext.Background())
	started := false
	defer func() {
		if !started {
			runtime.Stop()
		}
	}()

	conn, err := grpc.NewClient(entry.Spec.Address,
		grpc.WithTransportCredentials(dialCreds),
		grpc.WithStatsHandler(runtime),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxRemoteMessageSize),
			grpc.MaxCallSendMsgSize(maxRemoteMessageSize),
		),
		// Idle connections remain open; ConnEnd triggers reconnection.
		grpc.WithIdleTimeout(0),
		// Match older gRPC servers' default enforcement policy. Health RPCs
		// create traffic when idle and keep registration retrying on failures.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    5 * time.Minute,
			Timeout: 10 * time.Second,
		}),
	)
	if err != nil {
		return fmt.Errorf("plugin %s: dial %s: %w", entry.Name, entry.Spec.Address, err)
	}

	service := pluginAPI.NewPluginServiceClient(conn)
	runtime.conn = conn
	runtime.service = service

	// The plugin dials the host back-channel at the address it was given here.
	// A plugin may override the host default when it reaches Mission Control at
	// a different address than other plugins (e.g. a different network).
	hostGRPCAddress := entry.Spec.HostGRPCAddress
	if hostGRPCAddress == "" {
		hostGRPCAddress = commanderAPI.RemotePluginHostGRPCAddress
	}
	if hostGRPCAddress == "" {
		_ = conn.Close()
		return fmt.Errorf("plugin %s: no HostService back-channel address (set spec.hostGRPCAddress or --plugin-host-grpc-address)", entry.Name)
	}

	hostTLS, hostCACert, err := hostBackChannelTLS()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("plugin %s: %w", entry.Name, err)
	}

	runtime.register = func(ctx gocontext.Context) (*pluginAPI.PluginManifest, error) {
		ctx, cancel := gocontext.WithTimeout(ctx, remoteRegisterTimeout)
		defer cancel()
		conn.Connect()
		for {
			state := conn.GetState()
			if state == connectivity.Ready {
				break
			}
			if state == connectivity.Shutdown {
				return nil, fmt.Errorf("plugin connection closed")
			}
			if state == connectivity.Idle {
				conn.Connect()
			}
			if !conn.WaitForStateChange(ctx, state) {
				return nil, ctx.Err()
			}
		}
		generation := runtime.generation.Load()
		manifest, err := service.RegisterPlugin(ctx, &pluginAPI.RegisterRequest{
			HostProtocolVersion: uint32(pluginAPI.ProtocolVersion),
			HostGrpcAddress:     hostGRPCAddress,
			HostGrpcTls:         hostTLS,
			HostGrpcCaCert:      hostCACert,
		})
		if err == nil {
			runtime.registered.Store(generation)
		}
		return manifest, err
	}

	manifest, err := runtime.register(ctx)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("plugin %s RegisterPlugin: %w", entry.Name, err)
	}
	runtime.uiPort.Store(manifest.UiPort)

	started, err = plugin.DefaultRegistry.SetRuntimeIfAbsent(entry.ID, runtime)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if !started {
		_ = conn.Close()
		return nil
	}

	if !plugin.DefaultRegistry.SetManifestIfRuntime(entry.ID, runtime, manifest) {
		runtime.Stop()
		return nil
	}

	go runtime.reregisterOnReconnect(ctx)

	ctx.Logger.Infof("remote plugin %s loaded: address=%s version=%q ops=%d ui_port=%d",
		entry.Name, entry.Spec.Address, manifest.Version, len(manifest.Operations), manifest.UiPort)
	return nil
}

// reregisterOnReconnect handles transport events rather than sampled connection
// states. Health checks also notice silent failures without unsupported idle pings.
func (r *remoteRuntime) reregisterOnReconnect(ctx dutyContext.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.connEvents:
			r.conn.Connect()
			if r.generation.Load() != r.registered.Load() {
				r.reregister(ctx)
			}
		case <-ticker.C:
			healthCtx, cancel := gocontext.WithTimeout(r.ctx, 10*time.Second)
			_, err := r.service.Health(healthCtx, &pluginAPI.Empty{})
			cancel()
			if err != nil && status.Code(err) != codes.Unimplemented {
				r.reregister(ctx)
			}
		}
	}
}

// reregister retries RegisterPlugin until it succeeds or the runtime is stopped.
func (r *remoteRuntime) reregister(ctx dutyContext.Context) {
	for r.ctx.Err() == nil {
		manifest, err := r.register(r.ctx)
		if err == nil {
			if r.ctx.Err() != nil {
				return
			}
			r.uiPort.Store(manifest.UiPort)
			if !plugin.DefaultRegistry.SetManifestIfRuntime(r.id, r, manifest) {
				return
			}
			ctx.Logger.Infof("remote plugin %s registered again after reconnecting: version=%q", r.name, manifest.Version)
			if r.generation.Load() != r.registered.Load() {
				continue
			}
			return
		}
		ctx.Logger.Warnf("plugin %s: register again after reconnecting: %v", r.name, err)
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(remoteReregisterRetry):
		}
	}
}

// pluginTLSConfig builds the TLS config the host uses to reach a remote plugin
// over gRPC and HTTP. When spec.caCert is set the plugin's TLS certificate is
// verified against it; otherwise it returns nil and the host uses plaintext
// (only safe for same-host plugins).
func pluginTLSConfig(spec v1.PluginSpec) (*tls.Config, error) {
	if spec.CACert == "" {
		return nil, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(spec.CACert)) {
		return nil, fmt.Errorf("parse spec.caCert")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	// Present Mission Control's client certificate for plugins that require mTLS.
	if commanderAPI.PluginHostClientCertFile != "" && commanderAPI.PluginHostClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(commanderAPI.PluginHostClientCertFile, commanderAPI.PluginHostClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load plugin host client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// hostBackChannelTLS reports whether the host's HostService is served over TLS
// and, if so, the CA bundle a plugin should use to verify it (empty means the
// plugin falls back to the system roots).
func hostBackChannelTLS() (bool, string, error) {
	if commanderAPI.PluginHostTLSCertFile == "" || commanderAPI.PluginHostTLSKeyFile == "" {
		return false, "", nil
	}
	if commanderAPI.PluginHostTLSCAFile == "" {
		return true, "", nil
	}
	pem, err := os.ReadFile(commanderAPI.PluginHostTLSCAFile)
	if err != nil {
		return false, "", fmt.Errorf("read plugin host TLS CA: %w", err)
	}
	return true, string(pem), nil
}

const maxRemoteMessageSize = 64 * 1024 * 1024 // 64MB
