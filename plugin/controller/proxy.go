package controller

import (
	"fmt"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/auth"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
	"github.com/flanksource/incident-commander/plugin/supervisor"
)

func registerProxyRoutes(e *echo.Echo) {
	g := e.Group("/api/plugins")
	g.GET("/:name/ui", uiProxy)
	g.HEAD("/:name/ui", uiProxy)
	g.GET("/:name/ui/*", uiProxy)
	g.HEAD("/:name/ui/*", uiProxy)
	g.Any("/:name/proxy/:op", operationHTTPProxy)
	g.Any("/:name/proxy/:op/*", operationHTTPProxy)
}

// uiProxy reverse-proxies static plugin UI assets. Dynamic/plugin API calls
// must go through /api/plugins/:name/proxy/:op where :op is declared in the
// plugin manifest.
func uiProxy(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	entry, err := resolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if cfg := c.QueryParam("config_id"); cfg != "" && !selectorMatches(ctx, entry, cfg) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q is not enabled for config %s", pluginRef, cfg))
	}

	prefix := "/api/plugins/" + pluginRef + "/ui"
	pluginPath := strings.TrimPrefix(c.Request().URL.Path, prefix)
	if pluginPath == "" {
		pluginPath = "/"
	}
	if !allowedUIPath(entry, pluginPath) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin UI path %q not found", pluginPath))
	}

	return proxyToPluginHTTP(c, entry, pluginRef, prefix, "")
}

func operationHTTPProxy(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	op := c.Param("op")

	entry, err := resolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	def := operationDef(entry, op)
	if def == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q operation %q not found", pluginRef, op))
	}
	prefix := "/api/plugins/" + pluginRef + "/proxy/" + op
	operationPath := strings.TrimPrefix(c.Request().URL.Path, prefix)
	if operationPath == "" {
		operationPath = "/"
	}
	if !operationHTTPBindingAllowed(def, c.Request().Method, operationPath) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q operation %q does not allow HTTP %s %s", pluginRef, op, c.Request().Method, operationPath))
	}

	configID := c.QueryParam("config_id")
	if configID != "" && !selectorMatches(ctx, entry, configID) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q is not enabled for config %s", pluginRef, configID))
	}
	if err := enforcePluginInvokePermission(ctx, entry, op, configID); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	invocationToken, err := auth.MintPluginInvocationToken(lo.FromPtr(ctx.User()), entry.ID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "mint plugin invocation token"))
	}

	return proxyToPluginHTTP(c, entry, pluginRef, prefix, invocationToken)
}

func proxyToPluginHTTP(c echo.Context, entry *registry.Entry, pluginRef, prefix, invocationToken string) error {
	ctx := c.Request().Context().(dutyContext.Context)
	sup := supervisor.LookupSupervisor(entry.ID)
	if sup == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q not running", pluginRef))
	}
	port := sup.UIPort()
	if port == 0 {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINTERNAL).Errorf("plugin %q did not advertise a UI port", pluginRef))
	}

	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "parse plugin url"))
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			if invocationToken != "" {
				pr.Out.URL.Path = "/__mc/operations/" + c.Param("op") + strings.TrimPrefix(pr.In.URL.Path, prefix)
				pr.Out.Header.Set(pluginpb.PluginInvocationTokenMetadataKey, invocationToken)
			} else {
				pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
			}
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.Out.URL.RawPath = ""
			pr.Out.Header.Del("X-Mission-Control-User")
			pr.Out.Header.Del("X-Mission-Control-User-Email")
			pr.Out.Header.Del("X-Mission-Control-Config-Id")
			if u := ctx.User(); u != nil {
				pr.Out.Header.Set("X-Mission-Control-User", u.ID.String())
				if u.Email != "" {
					pr.Out.Header.Set("X-Mission-Control-User-Email", u.Email)
				}
			}
			if cfg := c.QueryParam("config_id"); cfg != "" {
				pr.Out.Header.Set("X-Mission-Control-Config-Id", cfg)
			}
		},
	}

	rp.ServeHTTP(c.Response().Writer, c.Request())
	return nil
}

func operationHTTPBindingAllowed(def *pluginpb.OperationDef, method, requestPath string) bool {
	requestPath = path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	for _, binding := range def.Http {
		if binding == nil || !strings.EqualFold(binding.Method, method) {
			continue
		}
		bindingPath := path.Clean("/" + strings.TrimPrefix(binding.Path, "/"))
		if binding.Path == "" {
			bindingPath = "/"
		}
		if requestPath == bindingPath {
			return true
		}
	}
	return false
}

func allowedUIPath(entry *registry.Entry, p string) bool {
	if p == "" || p == "/" {
		return true
	}
	clean := path.Clean("/" + strings.TrimPrefix(p, "/"))
	if strings.HasPrefix(clean, "/assets/") || strings.Contains(path.Base(clean), ".") {
		return true
	}
	if entry != nil && entry.Manifest != nil {
		for _, tab := range entry.Manifest.Tabs {
			if tab == nil {
				continue
			}
			tabPath := path.Clean("/" + strings.TrimPrefix(tab.Path, "/"))
			if clean == tabPath {
				return true
			}
		}
	}
	return false
}
