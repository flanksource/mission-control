package ui

import (
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	echov4 "github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Options struct {
	DevProxyTarget string
}

// RegisterRoutes mounts the embedded UI under /ui with SPA fallback.
//   - /ui/assets/* are hashed files served straight from the embedded dist
//     tree with long-lived cache headers.
//   - Every other /ui/* path returns index.html so the React router can
//     handle deep links client-side.
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
	e.GET("/ui/*", handleUI)
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

// handleUI serves files out of the embedded dist tree, falling through to
// the SPA shell for any path that doesn't map to a real asset.
func handleUI(c echov4.Context) error {
	rel := strings.TrimPrefix(c.Request().URL.Path, "/ui/")
	if rel == "" {
		return handleShell(c)
	}

	dist := distFS()
	if data, err := fs.ReadFile(dist, rel); err == nil {
		// Hashed assets (vite emits assets/<name>-<hash>.{js,css}) are
		// content-addressed, so they can be cached forever.
		if strings.HasPrefix(rel, "assets/") {
			c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		return c.Blob(http.StatusOK, contentType(rel), data)
	}

	return handleShell(c)
}

func handleShell(c echov4.Context) error {
	etag := `"` + BundleChecksum + `"`
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "no-cache")
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}

	data, err := fs.ReadFile(distFS(), "index.html")
	if err != nil {
		return err
	}
	return c.HTMLBlob(http.StatusOK, data)
}

func contentType(rel string) string {
	switch {
	case strings.HasSuffix(rel, ".js"), strings.HasSuffix(rel, ".mjs"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(rel, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(rel, ".map"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(rel, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(rel, ".png"):
		return "image/png"
	case strings.HasSuffix(rel, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(rel, ".woff"):
		return "font/woff"
	case strings.HasSuffix(rel, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(rel, ".json"):
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
