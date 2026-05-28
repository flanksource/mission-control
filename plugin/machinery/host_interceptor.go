package machinery

import (
	"context"
	"errors"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/plugin"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

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

type invocationClaimsContextKey struct{}

func invocationClaimsFromContext(ctx context.Context) (*auth.PluginInvocationClaims, bool) {
	claims, ok := ctx.Value(invocationClaimsContextKey{}).(*auth.PluginInvocationClaims)
	return claims, ok
}

func pluginEntryFromInvocation(ctx context.Context) (*plugin.Entry, error) {
	claims, ok := invocationClaimsFromContext(ctx)
	if !ok || claims.Plugin == uuid.Nil {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}
	entry := plugin.DefaultRegistry.Get(claims.Plugin)
	if entry == nil {
		return nil, status.Errorf(codes.NotFound, "plugin %s is not registered", claims.Plugin)
	}
	return entry, nil
}

func (s *Service) contextWithInvocation(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	values := md.Get(plugin.InvocationTokenGRPCMetadataKey)
	if len(values) == 0 || values[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	claims, err := auth.VerifyAnyPluginInvocationToken(values[0])
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid plugin invocation token: %v", err)
	}

	baseCtx := s.ctx.Wrap(ctx).WithSubject(claims.Subject).WithValue(invocationClaimsContextKey{}, claims)
	if api.UpstreamConf.Valid() {
		return baseCtx, nil
	}

	var person models.Person
	if err := baseCtx.DB().WithContext(ctx).Where("id = ?", claims.Subject).First(&person).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Errorf(codes.Unauthenticated, "plugin invocation subject %s not found", claims.Subject)
		}
		return nil, status.Errorf(codes.Unauthenticated, "plugin invocation subject %s: %v", claims.Subject, err)
	}

	return baseCtx.WithUser(&person), nil
}

func requiresInvocation(method string) bool {
	switch method {
	case plugin.HostService_GetConfigItem_FullMethodName,
		plugin.HostService_ListConfigs_FullMethodName,
		plugin.HostService_GetConnection_FullMethodName,
		plugin.HostService_InvokePlugin_FullMethodName:
		return true
	default:
		return false
	}
}
