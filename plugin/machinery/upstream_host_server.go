package machinery

import (
	"context"
	"fmt"
	"net"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyUpstream "github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/auth"
	pluginpb "github.com/flanksource/incident-commander/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type upstreamHostService struct {
	*Service
}

func (upstreamHostService) GetConnection(context.Context, *pluginpb.GetConnectionRequest) (*pluginpb.ResolvedConnection, error) {
	return nil, status.Error(codes.FailedPrecondition, "GetConnection must be served by the agent")
}

func StartUpstreamHostGRPCServer(ctx dutyContext.Context, port int) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	svc := NewGRPCService(ctx)
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(upstreamHostUnaryInterceptor(ctx)))
	pluginpb.RegisterHostServiceServer(grpcServer, upstreamHostService{Service: svc})

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	return grpcServer, nil
}

func upstreamHostUnaryInterceptor(base dutyContext.Context) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == pluginpb.HostService_GetConnection_FullMethodName {
			return nil, status.Error(codes.FailedPrecondition, "GetConnection must be served by the agent")
		}

		invocationCtx, err := upstreamHostContextWithInvocation(base, ctx)
		if err != nil {
			return nil, err
		}
		return handler(invocationCtx, req)
	}
}

func upstreamHostContextWithInvocation(base dutyContext.Context, ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	values := md.Get(pluginpb.InvocationTokenGRPCMetadataKey)
	if len(values) == 0 || values[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	claims, err := auth.VerifyAnyPluginInvocationToken(values[0])
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid plugin invocation token: %v", err)
	}

	dutyCtx := base.Wrap(ctx).WithSubject(claims.Subject).WithValue(invocationClaimsContextKey{}, claims)

	agentName := firstMetadataValue(md, dutyUpstream.AgentNameQueryParam)
	if agentName == "" {
		return nil, status.Error(codes.Unauthenticated, "agent name is required")
	}
	agent, err := dutyUpstream.GetOrCreateAgent(dutyCtx, agentName)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "agent %s: %v", agentName, err)
	}

	entry := pluginpb.DefaultRegistry.Get(claims.Plugin)
	if entry == nil || entry.AgentID == nil || *entry.AgentID != agent.ID {
		return nil, status.Error(codes.PermissionDenied, "agent does not own proxied plugin")
	}

	var person models.Person
	if err := dutyCtx.DB().WithContext(ctx).Where("id = ?", claims.Subject).First(&person).Error; err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "plugin invocation subject %s: %v", claims.Subject, err)
	}

	return dutyCtx.WithAgent(*agent).WithUser(&person), nil
}

func firstMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
