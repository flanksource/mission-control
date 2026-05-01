package ui

import (
	"net/http"
	"net/url"

	echov4 "github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Options struct {
	DevProxyTarget string
}

// RegisterRoutes mounts the embedded UI under /ui with SPA fallback.
// All paths under /ui/* return the same HTML shell; the React router
// handles deep links client-side.
func RegisterRoutes(e *echov4.Echo, opts Options) {
	e.GET("/", func(c echov4.Context) error {
		return c.Redirect(http.StatusFound, "/ui")
	})

	e.GET("/ui/logo.svg", handleLogo)
	e.GET("/ui/favicon.svg", handleFavicon)
	e.GET("/ui/openapi.json", handleOpenAPI)
	if opts.DevProxyTarget != "" {
		registerDevProxy(e, opts.DevProxyTarget)
		return
	}

	e.GET("/ui", handleShell)
	e.GET("/ui/*", handleShell)
}

func registerDevProxy(e *echov4.Echo, target string) {
	targetURL, err := url.Parse(target)
	if err != nil {
		e.Logger.Fatal(err)
	}

	proxy := middleware.ProxyWithConfig(middleware.ProxyConfig{
		Balancer: middleware.NewRoundRobinBalancer([]*middleware.ProxyTarget{
			{URL: targetURL},
		}),
	})(func(c echov4.Context) error {
		return nil
	})

	e.Any("/ui", proxy)
	e.Any("/ui/*", proxy)
}

func handleOpenAPI(c echov4.Context) error {
	return c.Blob(http.StatusOK, "application/json", OpenAPIJSON())
}

func handleLogo(c echov4.Context) error {
	return c.Blob(http.StatusOK, "image/svg+xml", logoSVG)
}

func handleFavicon(c echov4.Context) error {
	return c.Blob(http.StatusOK, "image/svg+xml", faviconSVG)
}

func handleShell(c echov4.Context) error {
	// Quote the checksum per RFC 7232 §2.3. Browsers revalidate by sending
	// If-None-Match on every request; we return 304 when it matches, which
	// skips retransmitting the 700KB bundle.
	etag := `"` + BundleChecksum + `"`
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "no-cache")
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}
	return c.HTML(http.StatusOK, pageHTML())
}
