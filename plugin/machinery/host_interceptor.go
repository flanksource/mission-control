package machinery

import (
	"context"
	"errors"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (s *Service) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !requiresInvocation(info.FullMethod) {
			resp, err := handler(ctx, req)
			return resp, grpcErrorFromError(err)
		}

		invocationCtx, err := s.contextWithInvocation(ctx)
		if err != nil {
			return nil, grpcErrorFromError(err)
		}
		resp, err := handler(invocationCtx, req)
		return resp, grpcErrorFromError(err)
	}
}

func grpcErrorFromError(err error) error {
	if err == nil {
		return nil
	}

	if _, ok := status.FromError(err); ok {
		return err
	}

	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, err.Error())
	}

	var oopsErr oops.OopsError
	if errors.As(err, &oopsErr) {
		if code, ok := dutyAPI.DomainCodeFromDBError(err); ok {
			return status.Error(grpcCodeFromDomainCode(code), oopsErr.Error())
		}

		code, _ := oopsErr.Code().(string)
		return status.Error(grpcCodeFromDomainCode(code), oopsErr.Error())
	}

	return status.Error(grpcCodeFromDomainCode(dutyAPI.ErrorCode(err)), dutyAPI.ErrorMessage(err))
}

func grpcCodeFromDomainCode(code string) codes.Code {
	switch code {
	case dutyAPI.ECONFLICT:
		return codes.AlreadyExists
	case dutyAPI.EINVALID:
		return codes.InvalidArgument
	case dutyAPI.ENOTFOUND:
		return codes.NotFound
	case dutyAPI.EFORBIDDEN:
		return codes.PermissionDenied
	case dutyAPI.ENOTIMPLEMENTED:
		return codes.Unimplemented
	case dutyAPI.EUNAUTHORIZED:
		return codes.Unauthenticated
	case dutyAPI.EINTERNAL:
		return codes.Internal
	default:
		return codes.Internal
	}
}

type invocationClaimsContextKey struct{}

func invocationClaimsFromContext(ctx context.Context) (*auth.PluginInvocationClaims, bool) {
	claims, ok := ctx.Value(invocationClaimsContextKey{}).(*auth.PluginInvocationClaims)
	return claims, ok
}

func (s *Service) contextWithInvocation(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	values := md.Get(api.InvocationTokenGRPCMetadataKey)
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

	return baseCtx.WithUser(&person).WithValue(invocationClaimsContextKey{}, claims), nil
}

func requiresInvocation(method string) bool {
	switch method {
	case api.HostService_GetConfigItem_FullMethodName,
		api.HostService_ListConfigs_FullMethodName,
		api.HostService_GetConnection_FullMethodName,
		api.HostService_InvokePlugin_FullMethodName:
		return true
	default:
		return false
	}
}
