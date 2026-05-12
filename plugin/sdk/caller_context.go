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
