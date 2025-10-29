package mcp

import (
	gocontext "context"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type dutyContextType string

var (
	dutyContextKey dutyContextType = "dutyContext"
)

type MCPServer struct {
	HTTPHandler http.Handler
	Server      *mcp.Server
}

func Server(ctx context.Context) *MCPServer {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "mission-control",
		Version: api.BuildVersion,
	}, nil)

	registerCatalog(s)
	registerConnections(s)
	registerHealthChecks(s)
	registerPlaybook(ctx, s)
	registerViews(ctx, s)

	registerJobs(ctx, s)

	logger.Infof("Registering /mcp routes")

	httpServer := &mcp.StreamableServerTransport{
		ContextMiddleware: func(ctx gocontext.Context, r *http.Request) gocontext.Context {
			dutyctx, ok := r.Context().(context.Context)
			if ok {
				return gocontext.WithValue(ctx, dutyContextKey, dutyctx)
			}
			// Return received context, should fail when controllers try to extract
			// duty context which is the desired behaviour
			return ctx
		},
	}

	return &MCPServer{
		Server:      s,
		HTTPHandler: httpServer,
	}
}
