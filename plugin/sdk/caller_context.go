package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/flanksource/incident-commander/plugin"
	"google.golang.org/grpc/metadata"
)

func invocationTokenFromIncomingContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(plugin.InvocationTokenGRPCMetadataKey)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func withInvocationToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, plugin.InvocationTokenGRPCMetadataKey, token)
}

type httpRequestContext struct {
	operation    string
	configItemID string
	roles        []string
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

// RolesFromContext returns the plugin roles attached to an HTTP operation request.
func RolesFromContext(ctx context.Context) []string {
	roles := httpRequestContextFromContext(ctx).roles
	return append([]string(nil), roles...)
}

// HasRole reports whether the HTTP operation request has the given plugin role.
func HasRole(ctx context.Context, role string) bool {
	return hasRole(httpRequestContextFromContext(ctx).roles, role)
}

func rolesFromInvocationToken(token string) []string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}

	var claims struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims.Roles
}

func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
