package mcp

import (
	gocontext "context"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
	"github.com/mark3labs/mcp-go/server"
)

type dutyContextType string

var (
	dutyContextKey dutyContextType = "dutyContext"
)

type MCPServer struct {
	HTTPHandler http.Handler
	Server      *server.MCPServer
}

func Server(ctx context.Context) *MCPServer {
	s := server.NewMCPServer("mission-control", api.BuildVersion,
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
	)

	registerCatalog(s)
	registerConnections(s)
	registerHealthChecks(s)
	registerPlaybook(ctx, s)
	registerViews(ctx, s)

	registerJobs(ctx, s)

	logger.Infof("Registering /mcp routes")

	httpServer := server.NewStreamableHTTPServer(s,
		server.WithHTTPContextFunc(func(ctx gocontext.Context, r *http.Request) gocontext.Context {
			dutyctx, ok := r.Context().(context.Context)
			if ok {
				return gocontext.WithValue(ctx, dutyContextKey, dutyctx)
			}
			// Return recevied context, should fail when controllers try to extract
			// duty context which is the desired behaviour
			return ctx
		}),
	)

	return &MCPServer{
		Server:      s,
		HTTPHandler: httpServer,
	}
}
