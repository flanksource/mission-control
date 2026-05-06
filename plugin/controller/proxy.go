package controller

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/plugin/supervisor"
)

// rewriteProxiedRequest mutates r in place so the plugin sees a clean path
// (the host's /api/plugins/<name>/ui prefix stripped) and so it knows the
// outward-facing prefix (X-Forwarded-Prefix) plus the calling user/catalog id.
//
// Extracted from the uiProxy closure so the rewrite can be unit-tested without
// spinning up a supervisor or RBAC enforcer.
func rewriteProxiedRequest(r *http.Request, prefix string, user *models.Person, configID string) {
	r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
	r.URL.RawPath = ""
	r.Header.Set("X-Forwarded-Prefix", prefix)
	if user != nil {
		r.Header.Set("X-Mission-Control-User", user.ID.String())
		if user.Email != "" {
			r.Header.Set("X-Mission-Control-User-Email", user.Email)
		}
	}
	if configID != "" {
		r.Header.Set("X-Mission-Control-Config-Id", configID)
	}
}

// init registers the UI proxy alongside the operations routes.
func init() {
	registerUIProxy = registerProxyRoutes
}

// registerProxyRoutes is wired in via the package-level registerUIProxy
// hook so the iframe proxy is always installed alongside the operations
// routes; this keeps the route registration in one file (controller.go)
// while letting us split the proxy implementation here.
func registerProxyRoutes(e *echo.Echo) {
	g := e.Group("/api/plugins/:name/ui")
	g.Any("", uiProxy)
	g.Any("/*", uiProxy)
}

// uiProxy reverse-proxies requests under /api/plugins/:name/ui/* to the
// plugin's HTTP server (whose port the plugin reports in PluginManifest.UiPort).
// It strips the prefix so the plugin's HTTPHandler sees a clean path.
func uiProxy(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	if err := authorizePluginUI(c, ctx); err != nil {
		return dutyAPI.WriteError(c, err)
	}
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
		rewriteProxiedRequest(r, prefix, ctx.User(), c.QueryParam("config_id"))
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		ctx.Logger.Warnf("plugin %s ui proxy: %s %s: %v", name, r.Method, r.URL.Path, err)
		http.Error(w, "plugin UI proxy error", http.StatusBadGateway)
	}

	rp.ServeHTTP(c.Response().Writer, c.Request())
	return nil
}

func authorizePluginUI(c echo.Context, ctx dutyContext.Context) error {
	if dutyRBAC.Enforcer() == nil {
		return nil
	}
	action := policy.ActionRead
	if methodRequiresPluginUpdate(c.Request()) {
		action = policy.ActionUpdate
	}
	if !dutyRBAC.CheckContext(ctx, policy.ObjectCatalog, action) {
		if u := ctx.User(); u != nil {
			c.Response().Header().Add("X-Rbac-Subject", u.ID.String())
		}
		c.Response().Header().Add("X-Rbac-Object", policy.ObjectCatalog)
		c.Response().Header().Add("X-Rbac-Action", action)
		return ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin UI requires catalog %s", action)
	}
	return nil
}

func methodRequiresPluginUpdate(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}
