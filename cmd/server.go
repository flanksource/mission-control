package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/utils"
)

const (
	HeaderCacheControl = "Cache-Control"
	CacheControlValue  = "public, max-age=2592000, immutable"
)

var cacheSuffixes = []string{
	".ico",
	".svg",
	".css",
	".js",
	".png",
}

var Serve = &cobra.Command{
	Use:    "serve",
	PreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		e := echo.New()
		// PostgREST needs to know how it is exposed to create the correct links
		db.HttpEndpoint = publicEndpoint + "/db"

		if !disablePostgrest {
			go db.StartPostgrest()
			forward(e, "/db", "http://localhost:3000")
		}
		e.Use(middleware.Logger())
		e.Use(ServerCache)
		if enableAuth {
			e.Use(utils.KratosMiddleware(kratosAdminAPI).Session)
		}
		forward(e, "/config", configDb)
		forward(e, "/canary", api.CanaryCheckerPath)
		forward(e, "/kratos", kratosAPI)
		forward(e, "/apm", apmHub)

		jobs.Start()
		go events.ListenForEvents()
		go responder.StartConfigSync()
		go responder.StartCommentSync()
		if err := e.Start(fmt.Sprintf(":%d", httpPort)); err != nil {
			e.Logger.Fatal(err)
		}
	},
}

func forward(e *echo.Echo, prefix string, target string) {
	_url, err := url.Parse(target)
	if err != nil {
		e.Logger.Fatal(err)
	}
	e.Group(prefix).Use(middleware.ProxyWithConfig(middleware.ProxyConfig{
		Rewrite: map[string]string{
			fmt.Sprintf("^%s/*", prefix): "/$1",
		},
		Balancer: middleware.NewRoundRobinBalancer([]*middleware.ProxyTarget{
			{
				URL: _url,
			},
		}),
	}))
}

func init() {
	ServerFlags(Serve.Flags())
}

// suffixesInItem checks if any of the suffixes are in the item.
func suffixesInItem(item string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(item, suffix) {
			return true
		}
	}
	return false
}

// ServerCache middleware adds a `Cache Control` header to the response.
func ServerCache(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if suffixesInItem(c.Request().RequestURI, cacheSuffixes) {
			c.Response().Header().Set(HeaderCacheControl, CacheControlValue)
		}
		return next(c)
	}
}
