package echo

import (
	"bytes"
	gocontext "context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/commons/http/middlewares"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	cutils "github.com/flanksource/commons/utils"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	dutyEcho "github.com/flanksource/duty/echo"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/schema/openapi"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/duty/telemetry"
	"github.com/flanksource/duty/topology"
	"github.com/labstack/echo-contrib/echoprometheus"
	echov4 "github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/flanksource/incident-commander/agent"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/utils"
	"github.com/flanksource/incident-commander/vars"
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

var otelShutdown func(gocontext.Context) error

var handlers []func(e *echov4.Echo)

func RegisterRoutes(fn func(e *echov4.Echo)) {
	handlers = append(handlers, fn)
}

func New(ctx context.Context) *echov4.Echo {
	ctx.ClearCache()
	e := echov4.New()
	e.HideBanner = true

	otelShutdown = telemetry.InitTracer()

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

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowCredentials: true,
		AllowOrigins:     AllowedCORS,
	}))

	if ctx.Properties().On(true, "access.log") {
		if logger.IsJsonLogs() {
			ctx.Infof("Enable JSON access logs")
			switch v := logger.StandardLogger().(type) {
			case logger.SlogLogger:
				e.Use(NewSlogLogger(ctx, v.Logger))
			case *logger.SlogLogger:
				e.Use(NewSlogLogger(ctx, v.Logger))
			default:
				e.Use(NewHttpSingleLineLogger(ctx, telemetryURLSkipper))
			}
		} else if ctx.Properties().On(false, "access.log.debug") {
			e.Use(NewHttpPrettyLogger(ctx))
		} else {
			e.Use(NewHttpSingleLineLogger(ctx, telemetryURLSkipper))
		}
	}

	dutyEcho.AddDebugHandlers(ctx, e, rbac.Authorization(policy.ObjectMonitor, policy.ActionUpdate))

	e.Use(ServerCache)

	e.GET("/kubeconfig", DownloadKubeConfig, rbac.Authorization(policy.ObjectKubernetesProxy, policy.ActionCreate))
	Forward(ctx, e, "/kubeproxy", "https://kubernetes.default.svc", KubeProxyTokenMiddleware)

	e.GET("/properties", dutyEcho.Properties)
	e.POST("/resources/search", SearchResources, rbac.Authorization(policy.ObjectCatalog, policy.ActionRead), RLSMiddleware)

	e.GET("/metrics", echoprometheus.NewHandlerWithConfig(echoprometheus.HandlerConfig{
		Gatherer: prom.DefaultGatherer,
	}))

	e.GET("/health", func(c echov4.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	personController := PersonController{kratos: auth.NewAPIClient(auth.KratosAPI)}
	e.POST("/people/update", personController.UpdatePerson, rbac.Authorization(policy.ObjectPeople, policy.ActionUpdate))
	e.DELETE("/people/:id", personController.DeletePerson, rbac.Authorization(policy.ObjectPeople, policy.ActionDelete))

	if dutyApi.DefaultConfig.Postgrest.URL != "" {
		Forward(ctx, e, "/db", dutyApi.DefaultConfig.Postgrest.URL,
			rbac.DbMiddleware(),
			db.SearchQueryTransformMiddleware(),
			postgrestInterceptor,
			postgrestTraceMiddleware,
		)
	}

	if vars.AuthMode != "" {
		if err := auth.Middleware(ctx, e); err != nil {
			shutdown.ShutdownAndExit(1, fmt.Sprintf("error setting up auth middleware: %v", err))
		}
	}

	e.GET("/canary/api/topology", topology.QueryHandler, RLSMiddleware)

	Forward(ctx, e, "/config", api.ConfigDB, rbac.Catalog("*"))
	Forward(ctx, e, "/apm", api.ApmHubPath, rbac.Authorization(policy.ObjectLogs, "*")) // Deprecated
	// webhooks perform their own auth
	Forward(ctx, e, "/canary/webhook", api.CanaryCheckerPath+"/webhook")
	Forward(ctx, e, "/canary", api.CanaryCheckerPath, rbac.Canary(""))
	// kratos performs its own auth
	Forward(ctx, e, "/kratos", auth.KratosAPI)

	auth.RegisterRoutes(e)
	rbac.RegisterRoutes(e)

	// Serve openapi schemas
	schemaServer, err := utils.HTTPFileserver(openapi.Schemas)
	if err != nil {
		logger.Fatalf("Error creating schema fileserver: %v", err)
	}
	e.GET("/schemas/*", echov4.WrapHandler(http.StripPrefix("/schemas/", schemaServer)))

	ctx.Infof("Registering %d handlers", len(handlers))
	for _, fn := range handlers {
		fn(e)
	}

	e.POST("/agent/generate", agent.GenerateAgent, rbac.Authorization(policy.ObjectAgent, policy.ActionUpdate))
	e.POST("/agent/token", agent.GenerateToken, rbac.Authorization(policy.ObjectAgent, policy.ActionUpdate))
	e.POST("/logs", logs.LogsHandler, rbac.Authorization(policy.ObjectLogs, policy.ActionRead))
	return e
}

func postgrestTraceMiddleware(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		ctx := c.Request().Context().(context.Context)

		table := strings.TrimPrefix(c.Request().URL.Path, "/db/")
		ctx.GetSpan().SetAttributes(attribute.String("db.table", table))

		for query, values := range c.Request().URL.Query() {
			ctx.GetSpan().SetAttributes(attribute.String(fmt.Sprintf("db.query.%s", query), values[0]))
		}

		return next(c)
	}
}

func postgrestInterceptor(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		path := strings.TrimPrefix(c.Request().URL.Path, "/db/")
		table := strings.Split(path, "?")[0] // Remove query parameters

		switch table {
		case "playbook_names":
			if c.Request().Method == http.MethodGet {
				ctx := c.Request().Context().(context.Context)
				q := c.Request().URL.Query()

				if err := applyPlaybookPermissionFilters(ctx, q); err != nil {
					return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EFORBIDDEN, "failed to apply permission filters: %v", err))
				}

				c.Request().URL.RawQuery = q.Encode()
				c.Request().RequestURI = c.Request().URL.String()
			}

		// For playbooks we need to validate the spec for create/update requests
		case "playbooks":
			method := c.Request().Method
			if method != http.MethodPost && method != http.MethodPatch {
				return next(c)
			}

			bodyBytes, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EINVALID, "failed to read request body: %v", err))
			}
			// Restore the body for the next handler
			c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			var requestData map[string]any
			if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
				return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EINVALID, "error unmarshaling request body: %v", err))
			}
			specValue, hasSpec := requestData["spec"]
			if !hasSpec {
				return next(c)
			}

			specBytes, err := json.Marshal(specValue)
			if err != nil {
				return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EINVALID, "error marshaling json: %v", err))
			}
			var spec v1.PlaybookSpec
			if err := json.Unmarshal(specBytes, &spec); err != nil {
				return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EINVALID, "invalid playbook spec: %v", err))
			}

			if err := spec.Validate(); err != nil {
				return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EINVALID, "playbook validation failed: %v", err))
			}
		}

		return next(c)
	}
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
	middlewares = append(middlewares, ModifyKratosRequestHeaders, proxyMiddleware(e, prefix, target))
	e.Group(prefix).Use(middlewares...)
}

func proxyMiddleware(e *echov4.Echo, prefix, targetURL string) echov4.MiddlewareFunc {
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
		// we use a new transport to override any tracing / instrumentation added in http.DefaultTransport
		proxyConfig.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}

		if properties.On(false, "log.kubeproxy") {
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

func Shutdown(e *echov4.Echo) {
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 1*time.Minute)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		e.Logger.Error(err)
	}

	if otelShutdown != nil {
		_ = otelShutdown(ctx)
	}
}

func Start(e *echov4.Echo, httpPort int) {
	if err := e.Start(fmt.Sprintf(":%d", httpPort)); err != nil && err != http.ErrServerClosed {
		e.Logger.Fatal(err)
	}

	listenAddr := fmt.Sprintf(":%d", httpPort)
	logger.Infof("Listening on %s", listenAddr)
	if err := e.Start(listenAddr); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to start server: %v", err))
	}
}

func RLSMiddleware(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		ctx := c.Request().Context().(context.Context)

		rlsPayload, err := auth.GetRLSPayload(ctx)
		if err != nil {
			return err
		}

		if ctx.Properties().On(false, "rls.debug") {
			ctx.Logger.WithValues("user", lo.FromPtr(ctx.User()).ID).Infof("RLS payload: %s", logger.Pretty(rlsPayload))
		}

		if rlsPayload.Disable {
			return next(c)
		}

		err = ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
			if err := rlsPayload.SetPostgresSessionRLS(txCtx.DB()); err != nil {
				return err
			}

			txCtx = txCtx.WithRLSPayload(rlsPayload)

			// set the context with the tx
			c.SetRequest(c.Request().WithContext(txCtx))

			return next(c)
		})

		return err
	}
}
