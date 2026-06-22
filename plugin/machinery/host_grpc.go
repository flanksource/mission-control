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
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
)

// connectionCacheTTL is how long a resolved connection stays cached on the host.
const connectionCacheTTL = 5 * time.Minute

type connKey struct {
	connectionID string
}

// Service is the host-side gRPC server.
type Service struct {
	pluginAPI.UnimplementedHostServiceServer

	ctx dutyContext.Context

	// connCache memoises named connection resolutions across calls within a single
	// plugin process. Authorization is checked before serving cached results.
	connCache *lru.LRU[connKey, *pluginAPI.ResolvedConnection]
}

// NewGRPCService creates a host Service.
func NewGRPCService(ctx dutyContext.Context) *Service {
	cache := lru.NewLRU[connKey, *pluginAPI.ResolvedConnection](256, nil, connectionCacheTTL)
	return &Service{
		ctx:       ctx,
		connCache: cache,
	}
}

// Register exposes the service on the given gRPC server.
func (s *Service) Register(g *grpc.Server) {
	pluginAPI.RegisterHostServiceServer(g, s)
}

func (s *Service) GetConfigItem(ctx context.Context, req *pluginAPI.GetConfigItemRequest) (*pluginAPI.ConfigItem, error) {
	if req.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	var out *pluginAPI.ConfigItem
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

func getConfigItem(ctx dutyContext.Context, queryCtx context.Context, id string) (*pluginAPI.ConfigItem, error) {
	var item models.ConfigItem
	if err := ctx.DB().WithContext(queryCtx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, fmt.Errorf("config item %s: %w", id, err)
	}
	return plugin.FromConfigItem(item)
}

func (s *Service) ListConfigs(ctx context.Context, req *pluginAPI.ListConfigsRequest) (*pluginAPI.ConfigItemList, error) {
	sel := plugin.ToDutyResourceSelector(req.Selector)
	out := &pluginAPI.ConfigItemList{}

	list := func(listCtx dutyContext.Context) error {
		items, err := query.FindConfigsByResourceSelector(listCtx, int(req.Limit), sel)
		if err != nil {
			return err
		}

		for i := range items {
			ci, err := plugin.FromConfigItem(items[i])
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

func (s *Service) GetConnection(ctx context.Context, req *pluginAPI.GetConnectionRequest) (*pluginAPI.ResolvedConnection, error) {
	if req.GetLookup() == nil {
		return nil, fmt.Errorf("connection lookup is required")
	}

	entry, err := pluginEntryFromInvocation(ctx)
	if err != nil {
		return nil, err
	}

	pluginCtx := s.ctx.Wrap(ctx).WithNamespace(entry.Namespace).WithSubject(pluginRBACSubject(entry))

	switch lookup := req.GetLookup().(type) {
	case *pluginAPI.GetConnectionRequest_Type:
		ref, ok := entry.Spec.Connections.Types[lookup.Type]
		if !ok || ref == "" {
			return nil, pluginCtx.Oops().Code(api.EINVALID).Errorf("plugin did not declare a %s connection mapping", lookup.Type)
		}
		return s.getConnectionByRef(pluginCtx, ref)

	case *pluginAPI.GetConnectionRequest_Label:
		ref, ok := entry.Spec.Connections.Labels[lookup.Label]
		if !ok || ref == "" {
			return nil, pluginCtx.Oops().Code(api.EINVALID).Errorf("plugin did not declare connection label %q", lookup.Label)
		}
		return s.getConnectionByRef(pluginCtx, ref)

	case *pluginAPI.GetConnectionRequest_ConfigItemId:
		return s.getConnectionForConfig(pluginCtx, lookup.ConfigItemId)

	case *pluginAPI.GetConnectionRequest_ConnectionId:
		return s.getConnectionByID(pluginCtx, lookup.ConnectionId)

	default:
		return nil, fmt.Errorf("connection lookup is required")
	}
}

func (s *Service) InvokePlugin(ctx context.Context, req *pluginAPI.InvokePluginRequest) (*pluginAPI.InvokeResponse, error) {
	dutyCtx := invocationDutyContext(s.ctx, ctx)
	if _, err := pluginEntryFromInvocation(ctx); err != nil {
		return nil, err
	}

	depth := 1
	// Plugins always act on behalf of the originating user, so thread the
	// user's subject (from the calling plugin's invocation token) down the
	// chain. The same subject authorizes the invoke and becomes the next
	// token's sub — no plugin-scoped invoke permission is required.
	userSubject := ""
	if claims, ok := invocationClaimsFromContext(ctx); ok {
		depth = claims.Depth + 1
		userSubject = claims.Subject
	}

	configID := req.ConfigItemId

	resp, _, err := InvokeOperation(dutyCtx, Request{
		Context:    ctx,
		PluginRef:  req.Plugin,
		Operation:  req.Operation,
		ParamsJSON: req.ParamsJson,
		Subject:    userSubject,
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

func (s *Service) Log(ctx context.Context, e *pluginAPI.LogEntry) (*pluginAPI.Empty, error) {
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
	return &pluginAPI.Empty{}, nil
}

// WriteArtifact / ReadArtifact are stubbed for the MVP.
// The artifact store integration is straight-forward (artifacts.Default in
// this codebase) but is not exercised by Phase 0–4 of the plugin plan.
func (s *Service) WriteArtifact(ctx context.Context, a *pluginAPI.Artifact) (*pluginAPI.ArtifactRef, error) {
	return nil, fmt.Errorf("WriteArtifact: not implemented")
}

func (s *Service) ReadArtifact(ctx context.Context, ref *pluginAPI.ArtifactRef) (*pluginAPI.Artifact, error) {
	return nil, fmt.Errorf("ReadArtifact: not implemented")
}
