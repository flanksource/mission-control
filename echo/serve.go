package echo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/flanksource/commons/logger"
	cutils "github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/schema/openapi"

	"github.com/flanksource/incident-commander/agent"
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

	echoLogConfig := middleware.DefaultLoggerConfig
	echoLogConfig.Skipper = telemetryURLSkipper

	e.Use(middleware.LoggerWithConfig(echoLogConfig))
	e.Use(ServerCache)

	e.GET("/kubeconfig", DownloadKubeConfig, rbac.Authorization(rbac.ObjectKubernetesProxy, rbac.ActionCreate))
	Forward(e, "/kube-proxy", "http://kubernetes.default.svc", KubeProxyTokenMiddleware)

	e.GET("/properties", Properties)
	e.POST("/resources/search", SearchResources)

	e.GET("/health", func(c echov4.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	e.GET("/snapshot/topology/:id", snapshot.Topology)
	e.GET("/snapshot/incident/:id", snapshot.Incident)
	e.GET("/snapshot/config/:id", snapshot.Config)

	e.POST("/auth/:id/update_state", auth.UpdateAccountState)
	e.POST("/auth/:id/properties", auth.UpdateAccountProperties)
	e.GET("/auth/whoami", auth.WhoAmI)

	e.POST("/rbac/:id/update_role", rbac.UpdateRoleForUser, rbac.Authorization(rbac.ObjectRBAC, rbac.ActionWrite))

	e.POST("/push/topology", push.PushTopology)

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
	e.POST("/agent/generate", agent.GenerateAgent, rbac.Authorization(rbac.ObjectAgentCreate, rbac.ActionWrite))
	e.POST("/logs", logs.LogsHandler)
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

func Forward(e *echov4.Echo, prefix string, target string, middlewares ...echov4.MiddlewareFunc) {
	middlewares = append(middlewares, ModifyKratosRequestHeaders, proxyMiddleware(e, prefix, target))
	e.Group(prefix).Use(middlewares...)
}

func proxyMiddleware(e *echov4.Echo, prefix, targetURL string) echov4.MiddlewareFunc {
	_url, err := url.Parse(targetURL)
	if err != nil {
		e.Logger.Fatal(err)
	}

	return middleware.ProxyWithConfig(middleware.ProxyConfig{
		Rewrite: map[string]string{
			fmt.Sprintf("^%s/*", prefix): "/$1",
		},
		Balancer: middleware.NewRoundRobinBalancer([]*middleware.ProxyTarget{{URL: _url}}),
	})
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

func SearchResources(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	var request query.SearchResourcesRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, err.Error()))
	}

	response, err := query.SearchResources(ctx, request)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func Properties(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	dbProperties, err := db.GetProperties(ctx)
	if err != nil {
		return api.WriteError(c, err)
	}

	var seen = make(map[string]struct{})

	var output = make([]map[string]string, 0)
	for _, p := range dbProperties {
		if _, ok := seen[p.Name]; ok {
			continue
		}

		output = append(output, map[string]string{
			"name":        p.Name,
			"value":       p.Value,
			"source":      "db",
			"type":        "",
			"description": "",
		})
	}

	for k, v := range context.Local {
		if _, ok := seen[k]; ok {
			continue
		}

		output = append(output, map[string]string{
			"name":        k,
			"value":       v,
			"source":      "local",
			"type":        "",
			"description": "",
		})
	}

	return c.JSON(http.StatusOK, output)
}
