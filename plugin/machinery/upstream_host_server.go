package machinery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyUpstream "github.com/flanksource/duty/upstream"
	commanderAPI "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// StartUpstreamHostGRPCServer opens the host's network HostService. Every call
// is gated on its invocation token resolving to a registered plugin (the
// registry is the whitelist); the plugin's Kind then selects how it is served:
//
//   - Remote plugins (spec.address) are served the full HostService — including
//     GetConnection — locally, using the same invocation-token trust model as
//     in-process (broker) plugins.
//   - Proxied plugins (agent-owned) keep the existing agent flow: GetConnection
//     must be resolved by the owning agent, everything else is authorized
//     against the agent that owns the plugin.
func StartUpstreamHostGRPCServer(ctx dutyContext.Context, port int) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	svc := NewGRPCService(ctx)
	serverOpts := []grpc.ServerOption{grpc.UnaryInterceptor(upstreamHostUnaryInterceptor(ctx, svc))}
	certSet := commanderAPI.PluginHostTLSCertFile != ""
	keySet := commanderAPI.PluginHostTLSKeyFile != ""
	if certSet != keySet {
		_ = lis.Close()
		return nil, fmt.Errorf("plugin HostService TLS requires both --plugin-host-tls-cert and --plugin-host-tls-key")
	}
	if certSet && keySet {
		tlsCfg, err := hostServerTLSConfig()
		if err != nil {
			_ = lis.Close()
			return nil, err
		}
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}

	grpcServer := grpc.NewServer(serverOpts...)
	pluginAPI.RegisterHostServiceServer(grpcServer, svc)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	return grpcServer, nil
}

// hostServerTLSConfig builds the TLS config for the plugin HostService gRPC
// server. When a client CA is configured it requires and verifies remote
// plugins' client certificates (mTLS).
func hostServerTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(commanderAPI.PluginHostTLSCertFile, commanderAPI.PluginHostTLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load plugin host TLS: %w", err)
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if commanderAPI.PluginHostTLSClientCAFile != "" {
		pem, err := os.ReadFile(commanderAPI.PluginHostTLSClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read plugin host client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse plugin host client CA")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

func upstreamHostUnaryInterceptor(base dutyContext.Context, svc *Service) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Whitelist gate: the call is only served if its invocation token names a
		// plugin that is registered. The plugin's Kind decides how it is served.
		entry, err := registeredPluginFromContext(ctx)
		if err != nil {
			return nil, err
		}

		if entry.Kind == pluginAPI.PluginKindRemote {
			invocationCtx, err := svc.contextWithInvocation(ctx)
			if err != nil {
				return nil, err
			}
			return handler(invocationCtx, req)
		}

		// Agent-owned (proxied) plugins: connections are resolved by the agent.
		if info.FullMethod == pluginAPI.HostService_GetConnection_FullMethodName {
			return nil, status.Error(codes.FailedPrecondition, "GetConnection must be served by the agent")
		}

		invocationCtx, err := upstreamHostContextWithInvocation(base, ctx)
		if err != nil {
			return nil, err
		}
		return handler(invocationCtx, req)
	}
}

// registeredPluginFromContext validates the invocation token carried in ctx and
// returns the registry entry for the plugin it names. It is the network
// HostService's whitelist: a token whose plugin has no registry entry — i.e. no
// Plugin spec/CRD — is rejected before any handler runs.
func registeredPluginFromContext(ctx context.Context) (*plugin.Entry, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}
	values := md.Get(pluginAPI.InvocationTokenGRPCMetadataKey)
	if len(values) == 0 || values[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	claims, err := plugin.ValidateHostInvocationToken(values[0])
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid plugin invocation token: %v", err)
	}

	entry := plugin.DefaultRegistry.Get(claims.Plugin)
	if entry == nil {
		return nil, status.Errorf(codes.PermissionDenied, "plugin %s is not registered", claims.Plugin)
	}
	return entry, nil
}

func upstreamHostContextWithInvocation(base dutyContext.Context, ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	values := md.Get(pluginAPI.InvocationTokenGRPCMetadataKey)
	if len(values) == 0 || values[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "plugin invocation token is required")
	}

	claims, err := plugin.ValidateInvocationToken(values[0])
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

	entry := plugin.DefaultRegistry.Get(claims.Plugin)
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
