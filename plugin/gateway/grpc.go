// Package host implements the HostService gRPC server — the back-channel
// that runs in the mission-control process and is dialed by every plugin
// during RegisterPlugin.
//
// All RPCs operate in the calling plugin's identity (matched via the
// peer-info that go-plugin's broker adds to the gRPC context).
package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"google.golang.org/grpc"

	"github.com/flanksource/incident-commander/auth"
	pluginpb "github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// connectionCacheTTL is how long a resolved connection stays cached on the host.
const connectionCacheTTL = 5 * time.Minute

type connKey struct {
	connectionID string
}

// Service is the host-side gRPC server. There is one per plugin process —
// the supervisor instantiates it during Start() so it can stamp the plugin
// id into requests for allowlist enforcement.
type Service struct {
	pluginpb.UnimplementedHostServiceServer

	pluginID uuid.UUID
	ctx      dutyContext.Context

	// connCache memoises named connection resolutions across calls within a single
	// plugin process. Authorization is checked before serving cached results.
	connCache *lru.LRU[connKey, *pluginpb.ResolvedConnection]

	invokePlugin Invoker
}

// NewGRPCService creates a host Service for one plugin id. Multiple plugins running
// concurrently get separate Services so the connection allowlist (read off
// the Plugin CRD via the registry) is enforced per-plugin.
func NewGRPCService(ctx dutyContext.Context, pluginID uuid.UUID) *Service {
	cache := lru.NewLRU[connKey, *pluginpb.ResolvedConnection](256, nil, connectionCacheTTL)
	return &Service{
		pluginID:  pluginID,
		ctx:       ctx,
		connCache: cache,
	}
}

// SetPluginInvoker configures the callback used by InvokePlugin.
func (s *Service) SetPluginInvoker(invoke Invoker) {
	s.invokePlugin = invoke
}

// Register exposes the service on the given gRPC server.
func (s *Service) Register(g *grpc.Server) {
	pluginpb.RegisterHostServiceServer(g, s)
}

func (s *Service) GetConfigItem(ctx context.Context, req *pluginpb.GetConfigItemRequest) (*pluginpb.ConfigItem, error) {
	if req.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	var out *pluginpb.ConfigItem
	err := auth.WithRLS(s.ctx.Wrap(ctx), func(rlsCtx dutyContext.Context) error {
		var item models.ConfigItem
		if err := rlsCtx.DB().WithContext(ctx).Where("id = ?", req.Id).First(&item).Error; err != nil {
			return fmt.Errorf("config item %s: %w", req.Id, err)
		}

		var err error
		out, err = pluginpb.FromConfigItem(item)
		return err
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (s *Service) ListConfigs(ctx context.Context, req *pluginpb.ListConfigsRequest) (*pluginpb.ConfigItemList, error) {
	sel := req.Selector.ToDuty()

	out := &pluginpb.ConfigItemList{}
	err := auth.WithRLS(s.ctx.Wrap(ctx), func(rlsCtx dutyContext.Context) error {
		items, err := query.FindConfigsByResourceSelector(rlsCtx, int(req.Limit), sel)
		if err != nil {
			return err
		}

		for i := range items {
			ci, err := pluginpb.FromConfigItem(items[i])
			if err != nil {
				return err
			}

			out.Items = append(out.Items, ci)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (s *Service) GetConnection(ctx context.Context, req *pluginpb.GetConnectionRequest) (*pluginpb.ResolvedConnection, error) {
	if req.GetLookup() == nil {
		return nil, fmt.Errorf("connection lookup is required")
	}

	entry := registry.Default.Get(s.pluginID)
	if entry == nil {
		return nil, fmt.Errorf("plugin %s is not registered", s.pluginID)
	}

	pluginCtx := s.ctx.Wrap(ctx).WithNamespace(entry.Namespace).WithSubject(pluginRBACSubject(entry))

	switch lookup := req.GetLookup().(type) {
	case *pluginpb.GetConnectionRequest_Type:
		ref, ok := entry.Spec.Connections.Types[lookup.Type]
		if !ok || ref == "" {
			return nil, pluginCtx.Oops().Code(api.EINVALID).Errorf("plugin did not declare a %s connection mapping", lookup.Type)
		}
		return s.getConnectionByRef(pluginCtx, ref)

	case *pluginpb.GetConnectionRequest_Label:
		ref, ok := entry.Spec.Connections.Labels[lookup.Label]
		if !ok || ref == "" {
			return nil, pluginCtx.Oops().Code(api.EINVALID).Errorf("plugin did not declare connection label %q", lookup.Label)
		}
		return s.getConnectionByRef(pluginCtx, ref)

	case *pluginpb.GetConnectionRequest_ConfigItemId:
		return s.getConnectionForConfig(pluginCtx, lookup.ConfigItemId)

	case *pluginpb.GetConnectionRequest_ConnectionId:
		return s.getConnectionByID(pluginCtx, lookup.ConnectionId)

	default:
		return nil, fmt.Errorf("connection lookup is required")
	}
}

func (s *Service) InvokePlugin(ctx context.Context, req *pluginpb.InvokePluginRequest) (*pluginpb.InvokeResponse, error) {
	dutyCtx := invocationDutyContext(s.ctx, ctx)
	source := registry.Default.Get(s.pluginID)
	if source == nil {
		return nil, fmt.Errorf("plugin %s is not registered", s.pluginID)
	}

	depth := 1
	if claims, ok := invocationClaimsFromContext(ctx); ok {
		depth = claims.Depth + 1
	}

	resp, _, err := Invoke(dutyCtx, Request{
		Context:      ctx,
		PluginRef:    req.Plugin,
		Operation:    req.Operation,
		ParamsJSON:   req.ParamsJson,
		ConfigItemID: req.ConfigItemId,
		User:         dutyCtx.User(),
		Subject:      PluginSubject(source.Namespace, source.Name),
		Depth:        depth,
		Deadline:     req.Deadline,
	}, s.invokePlugin)
	return resp, err
}

func invocationDutyContext(base dutyContext.Context, ctx context.Context) dutyContext.Context {
	if dutyCtx, ok := ctx.(dutyContext.Context); ok {
		return dutyCtx
	}
	return base.Wrap(ctx)
}

func (s *Service) Log(ctx context.Context, e *pluginpb.LogEntry) (*pluginpb.Empty, error) {
	logger := s.ctx.Logger
	args := make([]any, 0, len(e.Fields)*2)
	for k, v := range e.Fields {
		args = append(args, k, v)
	}
	prefix := fmt.Sprintf("[plugin %s] %s", s.pluginID, e.Message)
	switch e.Level {
	case "debug":
		logger.Debugf(prefix, args...)
	case "warn":
		logger.Warnf(prefix, args...)
	case "error":
		logger.Errorf(prefix, args...)
	default:
		logger.Infof(prefix, args...)
	}
	return &pluginpb.Empty{}, nil
}

// WriteArtifact / ReadArtifact are stubbed for the MVP.
// The artifact store integration is straight-forward (artifacts.Default in
// this codebase) but is not exercised by Phase 0–4 of the plugin plan.
func (s *Service) WriteArtifact(ctx context.Context, a *pluginpb.Artifact) (*pluginpb.ArtifactRef, error) {
	return nil, fmt.Errorf("WriteArtifact: not implemented")
}

func (s *Service) ReadArtifact(ctx context.Context, ref *pluginpb.ArtifactRef) (*pluginpb.Artifact, error) {
	return nil, fmt.Errorf("ReadArtifact: not implemented")
}
