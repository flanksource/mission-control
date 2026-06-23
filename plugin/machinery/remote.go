// Remote plugin runtime: dials a standalone plugin's gRPC server (spec.address)
// instead of supervising a local binary, and routes operation invocations to it.
package machinery

import (
	gocontext "context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	commanderAPI "github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
)

const remoteRegisterTimeout = 30 * time.Second

// remoteRuntime is the host-side handle for a plugin reachable over the network.
// It satisfies plugin.Runtime by forwarding calls to the plugin's gRPC server.
type remoteRuntime struct {
	conn    *grpc.ClientConn
	service pluginAPI.PluginServiceClient
	uiPort  uint32
}

func (r *remoteRuntime) Invoke(ctx gocontext.Context, req *pluginAPI.InvokeRequest) (*pluginAPI.InvokeResponse, error) {
	return r.service.Invoke(ctx, req)
}

func (r *remoteRuntime) UIPort() uint32 { return r.uiPort }

func (r *remoteRuntime) Stop() {
	if r.conn != nil {
		_ = r.conn.Close()
	}
}

// startRemotePlugin dials the plugin's gRPC server, completes RegisterPlugin
// (handing the plugin the host's HostService address so its callbacks work),
// and installs the runtime in the registry.
func startRemotePlugin(ctx dutyContext.Context, entry *plugin.Entry) error {
	dialCreds, err := pluginDialCredentials(entry.Spec)
	if err != nil {
		return fmt.Errorf("plugin %s: %w", entry.Name, err)
	}

	conn, err := grpc.NewClient(entry.Spec.Address,
		grpc.WithTransportCredentials(dialCreds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxRemoteMessageSize),
			grpc.MaxCallSendMsgSize(maxRemoteMessageSize),
		),
	)
	if err != nil {
		return fmt.Errorf("plugin %s: dial %s: %w", entry.Name, entry.Spec.Address, err)
	}

	service := pluginAPI.NewPluginServiceClient(conn)

	// The plugin dials the host back-channel at the address it was given here.
	// A plugin may override the host default when it reaches Mission Control at
	// a different address than other plugins (e.g. a different network).
	hostGRPCAddress := entry.Spec.HostGRPCAddress
	if hostGRPCAddress == "" {
		hostGRPCAddress = commanderAPI.RemotePluginHostGRPCAddress
	}

	hostTLS, hostCACert, err := hostBackChannelTLS()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("plugin %s: %w", entry.Name, err)
	}

	registerCtx, cancel := gocontext.WithTimeout(ctx, remoteRegisterTimeout)
	defer cancel()
	manifest, err := service.RegisterPlugin(registerCtx, &pluginAPI.RegisterRequest{
		HostProtocolVersion: uint32(pluginAPI.ProtocolVersion),
		HostGrpcAddress:     hostGRPCAddress,
		HostGrpcTls:         hostTLS,
		HostGrpcCaCert:      hostCACert,
	})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("plugin %s RegisterPlugin: %w", entry.Name, err)
	}

	runtime := &remoteRuntime{conn: conn, service: service, uiPort: manifest.UiPort}
	started, err := plugin.DefaultRegistry.SetRuntimeIfAbsent(entry.ID, runtime)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if !started {
		_ = conn.Close()
		return nil
	}

	if err := plugin.DefaultRegistry.SetManifest(entry.ID, manifest); err != nil {
		ctx.Logger.Warnf("plugin %s: register manifest: %v", entry.Name, err)
	}

	ctx.Logger.Infof("remote plugin %s loaded: address=%s version=%q ops=%d ui_port=%d",
		entry.Name, entry.Spec.Address, manifest.Version, len(manifest.Operations), manifest.UiPort)
	return nil
}

// pluginDialCredentials builds the transport credentials the host uses to dial
// a remote plugin's gRPC server. When spec.caCert is set the plugin's TLS
// certificate is verified against it; otherwise the dial is plaintext (only safe
// for same-host plugins).
func pluginDialCredentials(spec v1.PluginSpec) (credentials.TransportCredentials, error) {
	if spec.CACert == "" {
		return insecure.NewCredentials(), nil
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
	return credentials.NewTLS(cfg), nil
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
