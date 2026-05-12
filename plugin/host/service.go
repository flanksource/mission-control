// Package host implements the HostService gRPC server — the back-channel
// that runs in the mission-control process and is dialed by every plugin
// during RegisterPlugin.
//
// All RPCs operate in the calling plugin's identity (matched via the
// peer-info that go-plugin's broker adds to the gRPC context).
package host

import (
	"context"
	"fmt"
	"sync"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// connectionCacheTTL is how long a resolved connection stays cached on the
// host. Plugins re-fetch on cache miss, which is rare in practice (most
// operations make many calls within the TTL).
const connectionCacheTTL = 5 * time.Minute

type connKey struct {
	pluginID       uuid.UUID
	typ            string
	label          string
	configID       string
	rlsFingerprint string
}

// Service is the host-side gRPC server. There is one per plugin process —
// the supervisor instantiates it during Start() so it can stamp the plugin
// id into requests for allowlist enforcement and caching.
type Service struct {
	pluginpb.UnimplementedHostServiceServer

	pluginID uuid.UUID
	ctx      dutyContext.Context

	// connCache memoises GetConnection results across calls within a single
	// plugin process. Keyed by (plugin, type, label, configID).
	connCache *lru.LRU[connKey, *pluginpb.ResolvedConnection]
}

// New creates a host Service for one plugin id. Multiple plugins running
// concurrently get separate Services so the connection allowlist (read off
// the Plugin CRD via the registry) is enforced per-plugin.
func New(ctx dutyContext.Context, pluginID uuid.UUID) *Service {
	cache := lru.NewLRU[connKey, *pluginpb.ResolvedConnection](256, nil, connectionCacheTTL)
	return &Service{
		pluginID:  pluginID,
		ctx:       ctx,
		connCache: cache,
	}
}

// Register exposes the service on the given gRPC server.
func (s *Service) Register(g *grpc.Server) {
	pluginpb.RegisterHostServiceServer(g, s)
}

func (s *Service) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !requiresInvocation(info.FullMethod) {
			return handler(ctx, req)
		}

		invocationCtx, err := s.contextWithInvocation(ctx)
		if err != nil {
			return nil, err
		}
		return handler(invocationCtx, req)
	}
}

func requiresInvocation(method string) bool {
	switch method {
	case pluginpb.HostService_GetConfigItem_FullMethodName,
		pluginpb.HostService_ListConfigs_FullMethodName,
		pluginpb.HostService_GetConnection_FullMethodName:
		return true
	default:
		return false
	}
}

func (s *Service) contextWithInvocation(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	values := md.Get(pluginpb.PluginInvocationTokenMetadataKey)
	if len(values) == 0 || values[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	claims, err := auth.VerifyPluginInvocationToken(values[0], s.pluginID.String())
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid plugin invocation token: %v", err)
	}

	baseCtx := s.ctx.Wrap(ctx)
	var person models.Person
	if err := baseCtx.DB().WithContext(ctx).Where("id = ?", claims.Subject).First(&person).Error; err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "plugin invocation subject %s: %v", claims.Subject, err)
	}

	return baseCtx.WithUser(&person), nil
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

	baseCtx := s.ctx.Wrap(ctx)
	rlsPayload, err := auth.GetRLSPayload(baseCtx)
	if err != nil {
		return nil, err
	}

	entry := registry.Default.Get(s.pluginID)
	if entry == nil {
		return nil, fmt.Errorf("plugin %s is not registered", s.pluginID)
	}

	key := connKey{pluginID: s.pluginID, typ: req.GetType(), label: req.GetLabel(), configID: req.GetConfigItemId(), rlsFingerprint: rlsPayload.Fingerprint()}
	if cached, ok := s.connCache.Get(key); ok {
		return cached, nil
	}

	var resolved *pluginpb.ResolvedConnection
	err = auth.WithRLS(baseCtx, func(rlsCtx dutyContext.Context) error {
		var err error
		resolved, err = resolveConnection(rlsCtx, entry.Spec, req)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.connCache.Add(key, resolved)
	return resolved, nil
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

// _ keeps go vet from complaining about the import-only sync package usage
// in case a future revision drops the cache mutex.
var _ = sync.Mutex{}

// _ retains the v1 import for godoc cross-references; the package is used
// indirectly via registry.Default.Get(...).Spec.
var _ = v1.PluginSpec{}
