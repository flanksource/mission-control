package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spf13/cobra"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/schema/openapi"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/flanksource/incident-commander/snapshot"
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

		if !enableAuth {
			db.PostgresDBAnonRole = "postgrest_api"
		}
		if !disablePostgrest {
			go db.StartPostgrest()
			forward(e, "/db", "http://localhost:3000")
		}

		if externalPostgrestUri != "" {
			forward(e, "/db", externalPostgrestUri)
		}
		e.Use(middleware.Logger())
		e.Use(ServerCache)

		kratosHandler := auth.NewKratosHandler(kratosAPI, kratosAdminAPI, db.PostgRESTJWTSecret)
		if enableAuth {
			if _, err := kratosHandler.CreateAdminUser(context.Background()); err != nil {
				logger.Fatalf("Failed to created admin user: %v", err)
			}
			e.Use(kratosHandler.KratosMiddleware().Session)
		}
		e.POST("/auth/invite_user", kratosHandler.InviteUser)

		e.GET("/snapshot/topology/:id", snapshot.Topology)
		e.GET("/snapshot/incident/:id", snapshot.Incident)
		e.GET("/snapshot/config/:id", snapshot.Config)

		// Serve openapi schemas
		schemaServer, err := utils.HTTPFileserver(openapi.Schemas)
		if err != nil {
			logger.Fatalf("Error creating schema fileserver: %v", err)
		}
		e.GET("/schemas/*", echo.WrapHandler(http.StripPrefix("/schemas/", schemaServer)))

		forward(e, "/config", configDb)
		forward(e, "/canary", api.CanaryCheckerPath)
		forward(e, "/kratos", kratosAPI)
		forward(e, "/apm", api.ApmHubPath)

		go jobs.Start()
		go events.ListenForEvents()
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
