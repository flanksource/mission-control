package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/flanksource/incident-commander/upstream"
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
		e.HideBanner = true

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

		e.GET("/health", func(c echo.Context) error {
			if err := db.Pool.Ping(context.Background()); err != nil {
				return c.JSON(http.StatusInternalServerError, api.HTTPError{
					Error:   err.Error(),
					Message: "Failed to ping database",
				})
			}
			return c.JSON(http.StatusOK, api.HTTPSuccess{Message: "ok"})
		})

		kratosHandler := auth.NewKratosHandler(kratosAPI, kratosAdminAPI, db.PostgRESTJWTSecret)
		if enableAuth {
			if _, err := kratosHandler.CreateAdminUser(context.Background()); err != nil {
				logger.Fatalf("Failed to created admin user: %v", err)
			}

			middleware, err := kratosHandler.KratosMiddleware()
			if err != nil {
				logger.Fatalf("failed to initialize kratos middleware: %v", err)
			}
			e.Use(middleware.Session)
		}

		e.POST("/auth/invite_user", kratosHandler.InviteUser)

		e.GET("/snapshot/topology/:id", snapshot.Topology)
		e.GET("/snapshot/incident/:id", snapshot.Incident)
		e.GET("/snapshot/config/:id", snapshot.Config)

		e.POST("/auth/:id/update_state", auth.UpdateAccountState)
		e.POST("/auth/:id/properties", auth.UpdateAccountProperties)

		// Serve openapi schemas
		schemaServer, err := utils.HTTPFileserver(openapi.Schemas)
		if err != nil {
			logger.Fatalf("Error creating schema fileserver: %v", err)
		}
		e.GET("/schemas/*", echo.WrapHandler(http.StripPrefix("/schemas/", schemaServer)))

		if upstreamConfig.IsPartiallyFilled() {
			logger.Warnf("please ensure that all the required flags for upstream is supplied.")
		}
		e.POST("/upstream_push", upstream.PushUpstream)

		forward(e, "/config", configDb)
		forward(e, "/canary", api.CanaryCheckerPath)
		forward(e, "/kratos", kratosAPI)
		forward(e, "/apm", api.ApmHubPath) // Deprecated

		forwardWithMiddlewares(e, "/logs", api.ApmHubPath, logSearchMiddleware)

		go jobs.Start()

		eventHandlerConfig := events.Config{
			UpstreamConf: upstreamConfig,
		}
		go events.ListenForEvents(context.Background(), eventHandlerConfig)

		listenAddr := fmt.Sprintf(":%d", httpPort)
		logger.Infof("Listening on %s", listenAddr)
		if err := e.Start(listenAddr); err != nil {
			e.Logger.Fatal(err)
		}
	},
}

// logSearchMiddleware injects additional label and query params to the request
func logSearchMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		req := c.Request()
		if req.Method != http.MethodPost {
			return next(c)
		}

		var sp = make(map[string]any)
		if err := c.Bind(&sp); err != nil {
			return fmt.Errorf("failed to parse form: %w", err)
		}

		// TODO: Fetch the log selector (type, labels and name) from the component spec
		// Using a new duty method.
		// Hardcoding labels for now
		labels := map[string]string{
			"app.kubernetes.io/name": "kibana",
		}

		modifiedForm := injectLogSelectorToForm(labels, sp)
		if err := modifyReqBody(req, modifiedForm); err != nil {
			return fmt.Errorf("failed to write back search params: %w", err)
		}

		return next(c)
	}
}

func injectLogSelectorToForm(injectLabels map[string]string, form map[string]any) map[string]any {
	// Make sure label exists so we can inject our labels
	if _, ok := form["labels"]; !ok {
		form["labels"] = make(map[string]any)
	}

	if labels, ok := form["labels"].(map[string]any); ok {
		for k, v := range injectLabels {
			labels[k] = v
		}
		form["labels"] = labels
	}

	return form
}

func modifyReqBody(req *http.Request, sp any) error {
	encoded, err := json.Marshal(sp)
	if err != nil {
		return err
	}

	req.Body = io.NopCloser(bytes.NewReader(encoded))
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(encoded)))
	req.ContentLength = int64(len(encoded))
	return nil
}

// forwardWithMiddlewares forwards the request to the target just like forward()
// but attaches the given middlewares before proxying the request.
func forwardWithMiddlewares(e *echo.Echo, prefix string, target string, middlewares ...echo.MiddlewareFunc) {
	middlewares = append(middlewares, getProxyConfig(e, prefix, target))
	e.Group(prefix).Use(middlewares...)
}

func forward(e *echo.Echo, prefix string, target string) {
	e.Group(prefix).Use(getProxyConfig(e, prefix, target))
}

func getProxyConfig(e *echo.Echo, prefix, targetURL string) echo.MiddlewareFunc {
	_url, err := url.Parse(targetURL)
	if err != nil {
		e.Logger.Fatal(err)
	}

	proxyConf := middleware.ProxyConfig{
		Rewrite: map[string]string{
			fmt.Sprintf("^%s/*", prefix): "/$1",
		},
		Balancer: middleware.NewRoundRobinBalancer([]*middleware.ProxyTarget{{URL: _url}}),
	}
	return middleware.ProxyWithConfig(proxyConf)
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
