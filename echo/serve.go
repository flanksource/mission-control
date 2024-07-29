package echo

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/flanksource/commons/http/middlewares"
	"github.com/flanksource/commons/logger"
	cutils "github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/schema/openapi"
	"github.com/flanksource/incident-commander/agent"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/catalog"
	"github.com/flanksource/incident-commander/connection"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/push"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/snapshot"
	"github.com/flanksource/incident-commander/upstream"
	"github.com/flanksource/incident-commander/utils"
	"github.com/flanksource/incident-commander/vars"
	"github.com/labstack/echo-contrib/echoprometheus"
	echov4 "github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	prom "github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

const (
	HeaderCacheControl = "Cache-Control"
	CacheControlValue  = "public, max-age=2592000, immutable"
)

var (
	cacheSuffixes = []string{
		".ico",
		".svg",
		".css",
		".js",
		".png",
	}
	AllowedCORS []string
)

func New(ctx context.Context) *echov4.Echo {
	e := echov4.New()
	e.HideBanner = true

	e.Use(otelecho.Middleware("mission-control", otelecho.WithSkipper(telemetryURLSkipper)))

	e.Use(func(next echov4.HandlerFunc) echov4.HandlerFunc {
		return func(c echov4.Context) error {
			c.SetRequest(c.Request().WithContext(ctx.Wrap(c.Request().Context())))
			return next(c)
		}
	})

	e.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
		Registerer:                prom.DefaultRegisterer,
		Skipper:                   telemetryURLSkipper,
		DoNotUseRequestPathFor404: true,
	}))

	e.GET("/metrics", echoprometheus.NewHandlerWithConfig(echoprometheus.HandlerConfig{
		Gatherer: prom.DefaultGatherer,
	}))

	if ctx.Properties().On(true, "access.log") {
		echoLogConfig := middleware.DefaultLoggerConfig
		echoLogConfig.Skipper = telemetryURLSkipper

		e.Use(middleware.LoggerWithConfig(echoLogConfig))
	}
	e.Use(ServerCache)

	e.GET("/kubeconfig", DownloadKubeConfig, rbac.Authorization(rbac.ObjectKubernetesProxy, rbac.ActionCreate))
	Forward(ctx, e, "/kubeproxy", "https://kubernetes.default.svc", KubeProxyTokenMiddleware)

	e.GET("/properties", Properties)
	e.POST("/resources/search", SearchResources, rbac.Authorization(rbac.ObjectCatalog, rbac.ActionRead))

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowCredentials: true,
		AllowOrigins:     AllowedCORS,
	}))

	e.GET("/health", func(c echov4.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	if api.PostgrestURI != "" {
		Forward(ctx, e, "/db", api.PostgrestURI,
			rbac.DbMiddleware(),
			db.SearchQueryTransformMiddleware(),
		)
	}

	if vars.AuthMode != "" {
		db.PostgresDBAnonRole = "postgrest_api"
		if err := auth.Middleware(ctx, e); err != nil {
			logger.Fatalf(err.Error())
		}
	}

	Forward(ctx, e, "/config", api.ConfigDB, rbac.Catalog("*"))
	Forward(ctx, e, "/apm", api.ApmHubPath, rbac.Authorization(rbac.ObjectLogs, "*")) // Deprecated
	// webhooks perform their own auth
	Forward(ctx, e, "/canary/webhook", api.CanaryCheckerPath+"/webhook")
	Forward(ctx, e, "/canary", api.CanaryCheckerPath, rbac.Canary("*"))
	// kratos performs its own auth
	Forward(ctx, e, "/kratos", auth.KratosAPI)

	e.GET("/snapshot/topology/:id", snapshot.Topology, rbac.Topology(rbac.ActionWrite))
	e.GET("/snapshot/incident/:id", snapshot.Incident, rbac.Topology(rbac.ActionWrite))
	e.GET("/snapshot/config/:id", snapshot.Config, rbac.Catalog(rbac.ActionWrite))

	e.POST("/auth/:id/update_state", auth.UpdateAccountState)
	e.POST("/auth/:id/properties", auth.UpdateAccountProperties)
	e.GET("/auth/whoami", auth.WhoAmI)

	e.POST("/rbac/:id/update_role", rbac.UpdateRoleForUser, rbac.Authorization(rbac.ObjectRBAC, rbac.ActionWrite))
	e.GET("/rbac/dump", rbac.Dump, rbac.Authorization(rbac.ObjectRBAC, rbac.ActionRead))

	e.POST("/push/topology", push.PushTopology, rbac.Topology(rbac.ActionWrite))

	// Serve openapi schemas
	schemaServer, err := utils.HTTPFileserver(openapi.Schemas)
	if err != nil {
		logger.Fatalf("Error creating schema fileserver: %v", err)
	}
	e.GET("/schemas/*", echov4.WrapHandler(http.StripPrefix("/schemas/", schemaServer)))

	upstream.RegisterRoutes(e)
	catalog.RegisterRoutes(e)

	artifacts.RegisterRoutes(e, "artifacts")

	playbook.RegisterRoutes(e)
	connection.RegisterRoutes(e)
	e.POST("/agent/generate", agent.GenerateAgent, rbac.Authorization(rbac.ObjectAgent, rbac.ActionWrite))
	e.POST("/logs", logs.LogsHandler, rbac.Authorization(rbac.ObjectLogs, rbac.ActionRead))
	return e
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

func Forward(ctx context.Context, e *echov4.Echo, prefix string, target string, middlewares ...echov4.MiddlewareFunc) {
	middlewares = append(middlewares, ModifyKratosRequestHeaders, proxyMiddleware(ctx, e, prefix, target))
	e.Group(prefix).Use(middlewares...)
}

func proxyMiddleware(ctx context.Context, e *echov4.Echo, prefix, targetURL string) echov4.MiddlewareFunc {
	_url, err := url.Parse(targetURL)
	if err != nil {
		e.Logger.Fatal(err)
	}

	proxyConfig := middleware.ProxyConfig{
		Rewrite: map[string]string{
			fmt.Sprintf("^%s/*", prefix): "/$1",
		},
		Balancer: middleware.NewRoundRobinBalancer([]*middleware.ProxyTarget{{URL: _url}}),
	}

	if prefix == "/kubeproxy" {
		// Disable TLS verification for kubeproxy.
		newTransport := http.DefaultTransport.(*http.Transport).Clone()
		newTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		proxyConfig.Transport = newTransport

		if ctx.Properties().On(false, "log.kubeproxy") {
			traceConfig := middlewares.TraceConfig{
				MaxBodyLength:   1024,
				Timing:          true,
				ResponseHeaders: true,
				Headers:         true,
			}

			proxyConfig.Transport = middlewares.NewLogger(traceConfig)(proxyConfig.Transport)
		}
	}

	return middleware.ProxyWithConfig(proxyConfig)
}

// ServerCache middleware adds a `Cache Control` header to the response.
func ServerCache(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		if suffixesInItem(c.Request().RequestURI, cacheSuffixes) {
			c.Response().Header().Set(HeaderCacheControl, CacheControlValue)
		}
		return next(c)
	}
}

// telemetryURLSkipper ignores metrics route on some middleware
func telemetryURLSkipper(c echov4.Context) bool {
	pathsToSkip := []string{"/health", "/metrics"}
	return slices.Contains(pathsToSkip, c.Path())
}

func ModifyKratosRequestHeaders(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/kratos") {
			// Kratos requires the header X-Forwarded-Proto but Nginx sets it as "https,http"
			// This leads to URL malformation further upstream
			val := cutils.Coalesce(
				c.Request().Header.Get("X-Forwarded-Scheme"),
				c.Request().Header.Get("X-Scheme"),
				"https",
			)
			c.Request().Header.Set(echov4.HeaderXForwardedProto, val)

			// Need to remove the Authorization header set by our auth middleware for kratos
			// since it uses that header to extract token while performing certain actions
			c.Request().Header.Del(echov4.HeaderAuthorization)
		}
		return next(c)
	}
}
