package sdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

// pluginServer adapts the user's Plugin interface onto the generated
// pluginpb.PluginServiceServer. It also owns the back-channel connection
// to the host (HostClient) so InvokeCtx can hand it to operation handlers.
type pluginServer struct {
	pluginpb.UnimplementedPluginServiceServer

	impl    Plugin
	uiPort  uint32
	mu      sync.Mutex
	host    HostClient
	hostBrk *goplugin.GRPCBroker
	ops     map[string]Operation
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
	if s.hostBrk != nil && req.HostBrokerId != 0 {
		conn, err := s.hostBrk.Dial(req.HostBrokerId)
		if err != nil {
			return nil, fmt.Errorf("dial host broker: %w", err)
		}
		s.mu.Lock()
		s.host = newHostClient(conn)
		s.mu.Unlock()
	}

	manifest := s.impl.Manifest()
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
	if !ok {
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

	s.mu.Lock()
	host := s.host
	s.mu.Unlock()

	res, err := op.Handler(ctx, InvokeCtx{
		Operation:    req.Operation,
		ParamsJSON:   req.ParamsJson,
		ConfigItemID: req.ConfigItemId,
		Caller:       req.Caller,
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
