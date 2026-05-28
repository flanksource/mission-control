package machinery

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"google.golang.org/grpc"

	"github.com/flanksource/incident-commander/auth"
	pluginpb "github.com/flanksource/incident-commander/plugin"
)

// connectionCacheTTL is how long a resolved connection stays cached on the host.
const connectionCacheTTL = 5 * time.Minute

type connKey struct {
	connectionID string
}

// Service is the host-side gRPC server.
type Service struct {
	pluginpb.UnimplementedHostServiceServer

	ctx dutyContext.Context

	// connCache memoises named connection resolutions across calls within a single
	// plugin process. Authorization is checked before serving cached results.
	connCache *lru.LRU[connKey, *pluginpb.ResolvedConnection]
}

// NewGRPCService creates a host Service.
func NewGRPCService(ctx dutyContext.Context) *Service {
	cache := lru.NewLRU[connKey, *pluginpb.ResolvedConnection](256, nil, connectionCacheTTL)
	return &Service{
		ctx:       ctx,
		connCache: cache,
	}
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
		var err error
		out, err = getConfigItem(rlsCtx, ctx, req.Id)
		return err
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func getConfigItem(ctx dutyContext.Context, queryCtx context.Context, id string) (*pluginpb.ConfigItem, error) {
	var item models.ConfigItem
	if err := ctx.DB().WithContext(queryCtx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, fmt.Errorf("config item %s: %w", id, err)
	}
	return pluginpb.FromConfigItem(item)
}

func (s *Service) ListConfigs(ctx context.Context, req *pluginpb.ListConfigsRequest) (*pluginpb.ConfigItemList, error) {
	sel := req.Selector.ToDuty()
	out := &pluginpb.ConfigItemList{}

	list := func(listCtx dutyContext.Context) error {
		items, err := query.FindConfigsByResourceSelector(listCtx, int(req.Limit), sel)
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
	}

	err := auth.WithRLS(s.ctx.Wrap(ctx), list)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) GetConnection(ctx context.Context, req *pluginpb.GetConnectionRequest) (*pluginpb.ResolvedConnection, error) {
	if req.GetLookup() == nil {
		return nil, fmt.Errorf("connection lookup is required")
	}

	entry, err := pluginEntryFromInvocation(ctx)
	if err != nil {
		return nil, err
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
	source, err := pluginEntryFromInvocation(ctx)
	if err != nil {
		return nil, err
	}

	depth := 1
	if claims, ok := invocationClaimsFromContext(ctx); ok {
		depth = claims.Depth + 1
	}

	configID := req.ConfigItemId

	resp, _, err := InvokeOperation(dutyCtx, Request{
		Context:    ctx,
		PluginRef:  req.Plugin,
		Operation:  req.Operation,
		ParamsJSON: req.ParamsJson,
		Subject:    PluginSubject(source.Namespace, source.Name),
		Depth:      depth,
		Deadline:   req.Deadline,

		ConfigItemID: configID,
	})
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
	pluginID := "unknown"
	if claims, ok := invocationClaimsFromContext(ctx); ok {
		pluginID = claims.Plugin.String()
	}
	prefix := fmt.Sprintf("[plugin %s] %s", pluginID, e.Message)
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
