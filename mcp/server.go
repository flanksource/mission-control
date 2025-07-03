package mcp

import (
	gocontext "context"
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
	"github.com/mark3labs/mcp-go/server"
)

var (
	dutyContextKey = "dutyContext"
)

func Server(dutyctx context.Context) http.HandlerFunc {
	s := server.NewMCPServer("mission-control", api.BuildVersion,
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	registerCatalog(s)
	registerConnections(s)
	registerPlaybook(s)

	httpServer := server.NewStreamableHTTPServer(s,
		server.WithHTTPContextFunc(func(ctx gocontext.Context, r *http.Request) gocontext.Context {
			gctx, ok := r.Context().(context.Context)
			if ok {
				return gocontext.WithValue(ctx, dutyContextKey, gctx)
			}
			return gocontext.WithValue(ctx, dutyContextKey, dutyctx)
		}),
	)

	return httpServer.ServeHTTP
}
