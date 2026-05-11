package sdk

import (
	"context"

	"google.golang.org/grpc/metadata"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

type callerUserIDContextKey struct{}

func withCallerUserID(ctx context.Context, userID string) context.Context {
	if userID == "" {
		return ctx
	}
	ctx = context.WithValue(ctx, callerUserIDContextKey{}, userID)
	return metadata.AppendToOutgoingContext(ctx, pluginpb.CallerUserIDMetadataKey, userID)
}

func withCallerMetadata(ctx context.Context) context.Context {
	userID, _ := ctx.Value(callerUserIDContextKey{}).(string)
	if userID == "" {
		return ctx
	}
	if md, ok := metadata.FromOutgoingContext(ctx); ok && len(md.Get(pluginpb.CallerUserIDMetadataKey)) > 0 {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, pluginpb.CallerUserIDMetadataKey, userID)
}
