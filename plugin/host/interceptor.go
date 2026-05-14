package host

import (
	"context"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/plugin"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

func (s *Service) contextWithInvocation(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	values := md.Get(plugin.InvocationTokenGRPCMetadataKey)
	if len(values) == 0 || values[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	claims, err := auth.VerifyPluginInvocationToken(values[0], s.pluginID)
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
