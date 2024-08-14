package echo

import (
	"net"
	"net/http"
	"net/http/pprof"

	nethttp "net/http"

	"github.com/flanksource/commons/logger"
	"github.com/google/gops/agent"
	"github.com/labstack/echo/v4"
)

func init() {
	// disables default handlers registered by importing net/http/pprof.
	nethttp.DefaultServeMux = nethttp.NewServeMux()

	if err := agent.Listen(agent.Options{}); err != nil {
		logger.Errorf(err.Error())
	}
}

// restrictToLocalhost is a middleware that restricts access to localhost
func restrictToLocalhost(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		remoteIP := net.ParseIP(c.RealIP())
		if remoteIP == nil {
			return echo.NewHTTPError(http.StatusForbidden, "Invalid IP address")
		}

		if !remoteIP.IsLoopback() {
			return echo.NewHTTPError(http.StatusForbidden, "Access restricted to localhost")
		}

		return next(c)
	}
}

func AddDebugHandlers(e *echo.Echo) {
	// Add pprof routes with localhost restriction
	pprofGroup := e.Group("/debug/pprof")
	pprofGroup.Use(restrictToLocalhost)
	pprofGroup.GET("/*", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	pprofGroup.GET("/cmdline*", echo.WrapHandler(http.HandlerFunc(pprof.Cmdline)))
	pprofGroup.GET("/profile*", echo.WrapHandler(http.HandlerFunc(pprof.Profile)))
	pprofGroup.GET("/symbol*", echo.WrapHandler(http.HandlerFunc(pprof.Symbol)))
	pprofGroup.GET("/trace*", echo.WrapHandler(http.HandlerFunc(pprof.Trace)))
}
