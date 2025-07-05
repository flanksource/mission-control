package mcp

import (
	gocontext "context"
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
	"github.com/mark3labs/mcp-go/server"
)

type dutyContextType string

var (
	dutyContextKey dutyContextType = "dutyContext"
)

func Server() http.HandlerFunc {
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
			dutyctx, ok := r.Context().(context.Context)
			if ok {
				return gocontext.WithValue(ctx, dutyContextKey, dutyctx)
			}
			// Return recevied context, should fail when controllers try to extract
			// duty context which is the desired behaviour
			return ctx
		}),
	)

	return httpServer.ServeHTTP
}
