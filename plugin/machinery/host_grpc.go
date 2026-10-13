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

// pluginContext returns the context of a host callback: checked as the plugin that made it, in its namespace.
func (s *Service) pluginContext(ctx context.Context) (dutyContext.Context, *plugin.Entry, error) {
	entry, err := pluginEntryFromInvocation(ctx)
	if err != nil {
		return dutyContext.Context{}, nil, err
	}

	return s.ctx.Wrap(ctx).WithNamespace(entry.Namespace).WithSubject(pluginRBACSubject(entry)), entry, nil
}

func (s *Service) GetConfigItem(ctx context.Context, req *pluginAPI.GetConfigItemRequest) (*pluginAPI.ConfigItem, error) {
	if req.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	pluginCtx, _, err := s.pluginContext(ctx)
	if err != nil {
		return nil, err
	}

	if _, _, err := authorizeConfigRead(pluginCtx, pluginCtx.Subject(), req.Id); err != nil {
		return nil, err
	}

	return getConfigItem(pluginCtx, ctx, req.Id)
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

	pluginCtx, _, err := s.pluginContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := auth.WithRLS(pluginCtx, list); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) GetConnection(ctx context.Context, req *pluginAPI.GetConnectionRequest) (*pluginAPI.ResolvedConnection, error) {
	if req.GetLookup() == nil {
		return nil, fmt.Errorf("connection lookup is required")
	}

	pluginCtx, entry, err := s.pluginContext(ctx)
	if err != nil {
		return nil, err
	}

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

// InvokePlugin starts another plugin's operation as the calling plugin, never as the person who invoked the caller.
func (s *Service) InvokePlugin(ctx context.Context, req *pluginAPI.InvokePluginRequest) (*pluginAPI.InvokeResponse, error) {
	pluginCtx, entry, err := s.pluginContext(ctx)
	if err != nil {
		return nil, err
	}

	depth := 1
	user := ""
	if claims, ok := invocationClaimsFromContext(ctx); ok {
		depth = claims.Depth + 1
		user = claims.User
	}

	resp, _, err := InvokeOperation(pluginCtx, Request{
		Context:    ctx,
		PluginRef:  req.Plugin,
		Operation:  req.Operation,
		ParamsJSON: req.ParamsJson,
		Subject:    pluginRBACSubject(entry),
		User:       user,
		Depth:      depth,
		Deadline:   req.Deadline,

		ConfigItemID: req.ConfigItemId,
	})
	return resp, err
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
