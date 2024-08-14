package echo

import (
	"log/slog"

	"github.com/flanksource/commons/console"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/timer"
	"github.com/flanksource/duty/context"
	"github.com/henvic/httpretty"
	"github.com/labstack/echo/v4"
	echov4 "github.com/labstack/echo/v4"
	slogecho "github.com/samber/slog-echo"
)

func NewHttpSingleLineLogger(ctx context.Context, skipper func(c echov4.Context) bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {

		log := ctx.WithName("http").Logger

		return func(c echo.Context) error {
			err := next(c)
			if skipper != nil && skipper(c) {
				return err
			}

			timer := timer.NewTimer()
			status := c.Response().Status

			l := log.V(logger.Info)
			if status >= 500 {
				l = logger.V(logger.Error)
			} else if status >= 400 {
				l = logger.V(logger.Warn)
			}
			l.Infof("%s	%s	%d	%s", console.Greenf(c.Request().Method), c.Request().URL, status, timer)
			return err
		}
	}
}

func NewHttpPrettyLogger(ctx context.Context) echo.MiddlewareFunc {
	l := &httpretty.Logger{
		Time:           true,
		TLS:            false,
		RequestHeader:  ctx.Properties().On(false, "access.log.request.header"),
		RequestBody:    ctx.Properties().On(false, "access.log.request.body"),
		ResponseHeader: ctx.Properties().On(false, "access.log.request.header"),
		ResponseBody:   ctx.Properties().On(false, "access.log.response.body"),
		SkipSanitize:   ctx.Properties().On(false, "access.log.skip.sanitize"),
		Colors:         ctx.Properties().On(true, "access.log.colors"),
		Formatters:     []httpretty.Formatter{&httpretty.JSONFormatter{}},
	}

	return echo.WrapMiddleware(l.Middleware)
}

func NewSlogLogger(ctx context.Context, logger *slog.Logger) echo.MiddlewareFunc {
	slogecho.HiddenRequestHeaders["vary"] = struct{}{}
	// slogecho.HiddenRequestHeaders["vary"] = {}
	slogecho.HiddenRequestHeaders["accept-encoding"] = struct{}{}
	slogecho.RequestBodyMaxSize = ctx.Properties().Int("access.log.request.body.max", 2048)
	slogecho.ResponseBodyMaxSize = ctx.Properties().Int("access.log.response.body.max", 8192)
	// sanitize := ctx.Properties().On(false, "http.server.skip.sanitize"),
	return slogecho.NewWithConfig(logger, slogecho.Config{
		DefaultLevel:       slog.LevelInfo,
		ClientErrorLevel:   slog.LevelWarn,
		ServerErrorLevel:   slog.LevelError,
		WithUserAgent:      ctx.Properties().On(false, "access.log.userAgent"),
		WithRequestID:      ctx.Properties().On(false, "access.log.request.id"),
		WithRequestBody:    ctx.Properties().On(false, "access.log.request.body"),
		WithRequestHeader:  ctx.Properties().On(true, "access.log.request.header"),
		WithResponseBody:   ctx.Properties().On(false, "access.log.response.body"),
		WithResponseHeader: ctx.Properties().On(true, "access.log.request.header"),
		WithSpanID:         ctx.Properties().On(true, "access.log.spanId"),
		WithTraceID:        ctx.Properties().On(true, "access.log.traceId"),
		Filters:            []slogecho.Filter{slogecho.IgnorePathPrefix("/health", "/metrics")},
	})
}
