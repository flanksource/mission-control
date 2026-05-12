package sdk

import (
	"context"

	"google.golang.org/grpc/metadata"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

func withInvocationToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, pluginpb.PluginInvocationTokenMetadataKey, token)
}
