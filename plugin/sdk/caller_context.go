package sdk

import (
	"context"

	"google.golang.org/grpc/metadata"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

func invocationTokenFromIncomingContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(pluginpb.PluginInvocationTokenMetadataKey)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func withInvocationToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, pluginpb.PluginInvocationTokenMetadataKey, token)
}

type httpRequestContext struct {
	operation    string
	configItemID string
	host         HostClient
}

type httpRequestContextKey struct{}

func withHTTPRequestContext(ctx context.Context, req httpRequestContext) context.Context {
	return context.WithValue(ctx, httpRequestContextKey{}, req)
}

func httpRequestContextFromContext(ctx context.Context) httpRequestContext {
	if req, ok := ctx.Value(httpRequestContextKey{}).(httpRequestContext); ok {
		return req
	}
	return httpRequestContext{}
}

// HostClientFromContext returns the Mission Control host client attached to an
// HTTP operation request.
func HostClientFromContext(ctx context.Context) HostClient {
	return httpRequestContextFromContext(ctx).host
}

// OperationFromContext returns the operation name attached to an HTTP operation
// request.
func OperationFromContext(ctx context.Context) string {
	return httpRequestContextFromContext(ctx).operation
}

// ConfigItemIDFromContext returns the config_id attached to an HTTP operation
// request.
func ConfigItemIDFromContext(ctx context.Context) string {
	return httpRequestContextFromContext(ctx).configItemID
}
