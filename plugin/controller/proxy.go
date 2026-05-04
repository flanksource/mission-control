package controller

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/plugin/supervisor"
	"github.com/flanksource/incident-commander/rbac"
)

// init registers the UI proxy alongside the operations routes.
func init() {
	registerUIProxy = registerProxyRoutes
}

// registerProxyRoutes is wired in via the package-level registerUIProxy
// hook so the iframe proxy is always installed alongside the operations
// routes; this keeps the route registration in one file (controller.go)
// while letting us split the proxy implementation here.
func registerProxyRoutes(e *echo.Echo) {
	g := e.Group("/api/plugins/:name/ui",
		rbac.Authorization(policy.ObjectCatalog, policy.ActionRead),
	)
	g.Any("", uiProxy)
	g.Any("/*", uiProxy)
}

// uiProxy reverse-proxies requests under /api/plugins/:name/ui/* to the
// plugin's HTTP server (whose port the plugin reports in PluginManifest.UiPort).
// It strips the prefix so the plugin's HTTPHandler sees a clean path.
func uiProxy(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	name := c.Param("name")

	sup := supervisor.LookupSupervisor(name)
	if sup == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q not running", name))
	}
	port := sup.UIPort()
	if port == 0 {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINTERNAL).Errorf("plugin %q did not advertise a UI port", name))
	}

	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "parse plugin url"))
	}
	prefix := "/api/plugins/" + name + "/ui"

	rp := httputil.NewSingleHostReverseProxy(target)
	originalDirector := rp.Director
	rp.Director = func(r *http.Request) {
		originalDirector(r)
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		r.URL.RawPath = ""
		// Forward caller identity + the catalog id from the query string so
		// the plugin doesn't need to re-derive them.
		if u := ctx.User(); u != nil {
			r.Header.Set("X-Mission-Control-User", u.ID.String())
			if u.Email != "" {
				r.Header.Set("X-Mission-Control-User-Email", u.Email)
			}
		}
		if cfg := c.QueryParam("config_id"); cfg != "" {
			r.Header.Set("X-Mission-Control-Config-Id", cfg)
		}
	}

	rp.ServeHTTP(c.Response().Writer, c.Request())
	return nil
}
