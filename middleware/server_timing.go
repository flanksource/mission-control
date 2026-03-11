package middleware

import (
	"fmt"
	"time"

	echov4 "github.com/labstack/echo/v4"
)

// ServerTiming is a middleware that measures the total time taken by the
// HTTP handler chain and returns it in the standard Server-Timing response
// header (RFC 7611).
//
// The header value looks like: server;dur=12.34
// where dur is the wall-clock milliseconds spent inside the handler.
func ServerTiming(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		start := time.Now()
		c.Response().Before(func() {
			dur := time.Since(start)
			c.Response().Header().Set("Server-Timing", fmt.Sprintf("server;dur=%.2f", float64(dur.Microseconds())/1000.0))
		})
		return next(c)
	}
}
