package sdk

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pluginpb "github.com/flanksource/incident-commander/plugin/api"
)

// maxHostMessageSize bounds the back-channel gRPC messages a standalone plugin
// exchanges with the host, matching the host's own server-side limit.
const maxHostMessageSize = 64 * 1024 * 1024 // 64MB

// pluginServer adapts the user's Plugin interface onto the generated
// pluginpb.PluginServiceServer. It also owns the back-channel connection
// to the host (HostClient) so InvokeCtx can hand it to operation handlers.
type pluginServer struct {
	pluginpb.UnimplementedPluginServiceServer

	impl    Plugin
	uiPort  uint32
	mu      sync.Mutex
	hostBrk *goplugin.GRPCBroker
	ops     map[string]Operation

	// tlsCertFile/tlsKeyFile are the plugin's own certificate, presented as a
	// client certificate when dialing the host back-channel under mTLS.
	tlsCertFile string
	tlsKeyFile  string

	// a connection back to the mission-control gRPC server
	mcgPRCConn *grpc.ClientConn
}

func newPluginServer(impl Plugin, uiPort uint32) *pluginServer {
	ops := map[string]Operation{}
	for _, op := range impl.Operations() {
		if op.Def != nil {
			ops[op.Def.Name] = op
		}
	}
	return &pluginServer{impl: impl, uiPort: uiPort, ops: ops}
}

func (s *pluginServer) RegisterPlugin(ctx context.Context, req *pluginpb.RegisterRequest) (*pluginpb.PluginManifest, error) {
	switch {
	case s.hostBrk != nil && req.HostBrokerId != 0:
		// go-plugin subprocess mode: dial the host back-channel through the broker.
		conn, err := s.hostBrk.Dial(req.HostBrokerId)
		if err != nil {
			return nil, fmt.Errorf("dial host broker: %w", err)
		}
		s.mu.Lock()
		s.mcgPRCConn = conn
		s.mu.Unlock()

	case req.HostGrpcAddress != "":
		// Standalone/remote mode: there is no broker, so dial the host's
		// HostService directly over the network. The invocation token added by
		// each HostClient call authenticates these requests.
		creds, err := hostTransportCredentials(req, s.tlsCertFile, s.tlsKeyFile)
		if err != nil {
			return nil, err
		}
		conn, err := grpc.NewClient(req.HostGrpcAddress,
			grpc.WithTransportCredentials(creds),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(maxHostMessageSize),
				grpc.MaxCallSendMsgSize(maxHostMessageSize),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("dial host %s: %w", req.HostGrpcAddress, err)
		}
		s.mu.Lock()
		s.mcgPRCConn = conn
		s.mu.Unlock()
	}

	manifest := s.impl.Manifest()
	return s.finishRegister(manifest)
}

// hostTransportCredentials builds the transport credentials a standalone plugin
// uses to dial the host back-channel. It is plaintext unless the host asked for
// TLS, in which case the host's certificate is verified against the supplied CA
// bundle (or the system roots when none is given). When the plugin has its own
// certificate it is presented as a client certificate so the host can enforce
// mTLS.
func hostTransportCredentials(req *pluginpb.RegisterRequest, certFile, keyFile string) (credentials.TransportCredentials, error) {
	if !req.HostGrpcTls {
		return insecure.NewCredentials(), nil
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if req.HostGrpcCaCert != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(req.HostGrpcCaCert)) {
			return nil, fmt.Errorf("parse host CA cert")
		}
		cfg.RootCAs = pool
	}
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load plugin client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(cfg), nil
}

func (s *pluginServer) finishRegister(manifest *pluginpb.PluginManifest) (*pluginpb.PluginManifest, error) {
	if manifest == nil {
		return nil, fmt.Errorf("plugin Manifest() returned nil")
	}
	if manifest.Name == "" {
		return nil, fmt.Errorf("plugin Manifest().Name is required")
	}
	if manifest.Version == "" {
		return nil, fmt.Errorf("plugin %s Manifest().Version is required (build with -ldflags '-X main.Version=...')", manifest.Name)
	}
	manifest.UiPort = s.uiPort
	// Materialize operations from the live registry so a plugin doesn't have
	// to declare them twice.
	if len(manifest.Operations) == 0 {
		for _, op := range s.ops {
			manifest.Operations = append(manifest.Operations, op.Def)
		}
	}
	return manifest, nil
}

func (s *pluginServer) Configure(ctx context.Context, req *pluginpb.ConfigureRequest) (*pluginpb.ConfigureResponse, error) {
	settings, err := settingsFromStruct(req.Settings)
	if err != nil {
		return nil, err
	}
	if err := s.impl.Configure(ctx, settings); err != nil {
		return nil, err
	}
	return &pluginpb.ConfigureResponse{}, nil
}

func (s *pluginServer) ListOperations(ctx context.Context, _ *pluginpb.Empty) (*pluginpb.OperationList, error) {
	out := &pluginpb.OperationList{}
	for _, op := range s.ops {
		out.Operations = append(out.Operations, op.Def)
	}
	return out, nil
}

func (s *pluginServer) Invoke(ctx context.Context, req *pluginpb.InvokeRequest) (*pluginpb.InvokeResponse, error) {
	op, ok := s.ops[req.Operation]
	if !ok || op.Handler == nil {
		return &pluginpb.InvokeResponse{
			ErrorCode:    "UNKNOWN_OPERATION",
			ErrorMessage: fmt.Sprintf("unknown operation %q", req.Operation),
		}, nil
	}

	if req.Deadline != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, req.Deadline.AsTime())
		defer cancel()
	}

	token := invocationTokenFromIncomingContext(ctx)
	s.mu.Lock()
	host := newHostClient(s.mcgPRCConn, token)
	s.mu.Unlock()

	res, err := op.Handler(ctx, InvokeCtx{
		Operation:    req.Operation,
		ParamsJSON:   req.ParamsJson,
		ConfigItemID: req.ConfigItemId,
		Roles:        rolesFromInvocationToken(token),
		Host:         host,
	})
	if err != nil {
		return &pluginpb.InvokeResponse{
			ErrorCode:    "HANDLER_ERROR",
			ErrorMessage: err.Error(),
		}, nil
	}

	body, err := ClickyResult(res)
	if err != nil {
		return nil, err
	}

	mime := op.Def.ResultMime
	if mime == "" {
		mime = ClickyResultMimeType
	}
	return &pluginpb.InvokeResponse{Result: body, Mime: mime}, nil
}

func (s *pluginServer) httpOperationsHandler() http.Handler {
	mux := http.NewServeMux()
	for _, op := range s.ops {
		if op.Def == nil || op.HTTPHandler == nil {
			continue
		}
		operationName := op.Def.Name
		for _, binding := range op.Def.Http {
			if binding == nil || strings.TrimSpace(binding.Method) == "" {
				continue
			}
			pattern := strings.ToUpper(binding.Method) + " /__mc/operations/" + operationName
			mux.Handle(pattern, s.httpOperationMiddleware(operationName, op.HTTPHandler))
		}
	}
	return mux
}

func (s *pluginServer) httpOperationMiddleware(operationName string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(pluginpb.InvocationTokenHTTPHeader)
		s.mu.Lock()
		host := newHostClient(s.mcgPRCConn, token)
		s.mu.Unlock()

		ctx := withHTTPRequestContext(r.Context(), httpRequestContext{
			operation:    operationName,
			configItemID: r.URL.Query().Get("config_id"),
			roles:        rolesFromInvocationToken(token),
			host:         host,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *pluginServer) Health(ctx context.Context, _ *pluginpb.Empty) (*pluginpb.HealthStatus, error) {
	return &pluginpb.HealthStatus{Ok: true}, nil
}

func (s *pluginServer) Shutdown(ctx context.Context, _ *pluginpb.Empty) (*pluginpb.Empty, error) {
	go func() {
		// Give the response a moment to flush.
		time.Sleep(50 * time.Millisecond)
		shutdownSignal()
	}()
	return &pluginpb.Empty{}, nil
}

// grpcAdapter wires pluginServer into the goplugin GRPCPlugin so we can grab
// the broker that the host uses for its back-channel.
type grpcAdapter struct {
	goplugin.Plugin
	srv *pluginServer
}

func (a *grpcAdapter) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	a.srv.hostBrk = broker
	pluginpb.RegisterPluginServiceServer(s, a.srv)
	return nil
}

func (a *grpcAdapter) GRPCClient(ctx context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	// Plugins never act as a client of themselves.
	return nil, nil
}
