package mcp

import (
	gocontext "context"
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	echov4 "github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
)

type dutyContextType string

var (
	dutyContextKey dutyContextType = "dutyContext"
)

type MCPServer struct {
	HTTPHandler http.Handler
	Server      *server.MCPServer
}

// AuthMiddleware only allows a person if they have at least one mcp:run permission
func AuthMiddleware(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		ctx := c.Request().Context().(context.Context)

		if roles, err := rbac.RolesForUser(ctx.Subject()); err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
		} else if lo.Contains(roles, policy.RoleAdmin) {
			return next(c)
		}

		permissions, err := rbac.PermsForUser(ctx.Subject())
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
		}

		_, ok := lo.Find(permissions, func(perm policy.Permission) bool {
			return perm.Action == policy.ActionMCPRun && !perm.Deny
		})
		if !ok {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "forbidden: user %s does not have mcp:run permission", ctx.Subject()))
		}

		return next(c)
	}
}

func Server(ctx context.Context, serverOpts ...server.StreamableHTTPOption) *MCPServer {
	hooks := &server.Hooks{}

	s := server.NewMCPServer("mission-control", api.BuildVersion,
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
		server.WithHooks(hooks),
	)

	hooks.AddOnRegisterSession(func(goctx gocontext.Context, session server.ClientSession) {
		if err := addPlaybooksAsTool(goctx, s, session); err != nil {
			logger.Errorf("error on addPlaybooksAsTool for session %s: %v", session.SessionID(), err)
		}
	})

	registerArtifacts(s)
	registerCatalog(s)
	registerConnections(s)
	registerHealthChecks(s)
	registerPlaybook(s)
	registerViews(ctx, s)
	registerNotifications(s)

	registerJobs(ctx, s)

	logger.Infof("Registering /mcp routes")

	serverOpts = append(serverOpts, server.WithHTTPContextFunc(func(ctx gocontext.Context, r *http.Request) gocontext.Context {
		dutyctx, ok := r.Context().(context.Context)
		if ok {
			return gocontext.WithValue(ctx, dutyContextKey, dutyctx)
		}
		// Return recevied context, should fail when controllers try to extract
		// duty context which is the desired behaviour
		return ctx
	}),
	)

	httpServer := server.NewStreamableHTTPServer(s, serverOpts...)

	return &MCPServer{
		Server:      s,
		HTTPHandler: httpServer,
	}
}
