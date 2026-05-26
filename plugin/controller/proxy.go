package controller

import (
	"fmt"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/plugin"
	pluginpb "github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/registry"
	pluginruntime "github.com/flanksource/incident-commander/plugin/runtime"
	"github.com/flanksource/incident-commander/plugin/supervisor"
	"github.com/flanksource/incident-commander/rbac"
)

func registerProxyRoutes(e *echo.Echo) {
	g := e.Group("/api/plugins")
	uiAuth := rbac.Authorization(policy.ObjectCatalog, policy.ActionRead)
	g.GET("/:name/ui", uiProxy, uiAuth)
	g.GET("/:name/ui/*", uiProxy, uiAuth)
	g.Any("/:name/proxy/:op", operationHTTPProxy)
}

// uiProxy reverse-proxies static plugin UI assets. Dynamic/plugin API calls
// must go through /api/plugins/:name/proxy/:op where :op is declared in the
// plugin manifest.
func uiProxy(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	entry, err := pluginruntime.ResolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if cfg := c.QueryParam("config_id"); cfg != "" {
		matches, err := pluginruntime.SelectorMatches(ctx, entry, cfg)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
		}
		if !matches {
			return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q is not enabled for config %s", pluginRef, cfg))
		}
	}

	prefix := "/api/plugins/" + pluginRef + "/ui"
	pluginPath := strings.TrimPrefix(c.Request().URL.Path, prefix)
	if pluginPath == "" {
		pluginPath = "/"
	}
	if !allowedUIPath(entry, pluginPath) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin UI path %q not found", pluginPath))
	}

	return proxyToPluginUI(c, entry, pluginRef, prefix)
}

func operationHTTPProxy(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	op := c.Param("op")

	entry, err := pluginruntime.ResolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	def := pluginruntime.OperationDef(entry, op)
	if def == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q operation %q not found", pluginRef, op))
	}
	if !operationHTTPBindingAllowed(def, c.Request().Method) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q operation %q does not allow HTTP %s", pluginRef, op, c.Request().Method))
	}

	configID := c.QueryParam("config_id")
	if configID == "" {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("config_id is required"))
	}
	configUUID, err := uuid.Parse(configID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("config_id is invalid"))
	}
	matches, err := pluginruntime.SelectorMatches(ctx, entry, configID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	if !matches {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q is not enabled for config %s", pluginRef, configID))
	}
	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("not logged in"))
	}
	subject := user.ID.String()
	if err := pluginruntime.EnforceInvokePermission(ctx, subject, entry, op, configID); err != nil {
		return dutyAPI.WriteError(c, err)
	}
	roles, err := pluginRolesForUser(ctx, entry, configID)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	invocationToken, err := auth.MintPluginInvocationToken(*user, entry.ID, roles...)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "mint plugin invocation token"))
	}

	paramsHash := httpParamsHash(c.Request().Method, c.QueryParams())
	if err := proxyToPluginOperation(c, entry, pluginRef, op, invocationToken); err != nil {
		return err
	}

	recordPluginInvocation(ctx, entry, op, configUUID, "http", c.Request().Method, paramsHash, "", c.Request(), nil)

	return nil
}

func proxyToPluginUI(c echo.Context, entry *registry.Entry, pluginRef, prefix string) error {
	target, err := pluginHTTPURL(c, entry, pluginRef)
	if err != nil {
		return err
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = pluginUITargetPath(prefix, pr.In.URL.Path)
			pr.Out.URL.RawPath = ""
		},
	}

	rp.ServeHTTP(c.Response().Writer, c.Request())
	return nil
}

func proxyToPluginOperation(c echo.Context, entry *registry.Entry, pluginRef, op, invocationToken string) error {
	target, err := pluginHTTPURL(c, entry, pluginRef)
	if err != nil {
		return err
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = pluginOperationTargetPath(op)
			pr.Out.Header.Set(plugin.InvocationTokenHTTPHeader, invocationToken)
			pr.Out.URL.RawPath = ""
		},
	}

	rp.ServeHTTP(c.Response().Writer, c.Request())
	return nil
}

func pluginHTTPURL(c echo.Context, entry *registry.Entry, pluginRef string) (*url.URL, error) {
	ctx := c.Request().Context().(dutyContext.Context)
	sup := supervisor.LookupSupervisor(entry.ID)
	if sup == nil {
		return nil, dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q not running", pluginRef))
	}
	port := sup.UIPort()
	if port == 0 {
		return nil, dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINTERNAL).Errorf("plugin %q did not advertise a UI port", pluginRef))
	}

	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return nil, dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "parse plugin url"))
	}
	return target, nil
}

func pluginOperationTargetPath(op string) string {
	return "/__mc/operations/" + op
}

func pluginUITargetPath(prefix, requestPath string) string {
	pluginPath := strings.TrimPrefix(requestPath, prefix)
	if pluginPath == "" {
		pluginPath = "/"
	}
	return "/__mc/ui" + pluginPath
}

func operationHTTPBindingAllowed(def *pluginpb.OperationDef, method string) bool {
	for _, binding := range def.Http {
		if binding != nil && strings.EqualFold(binding.Method, method) {
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
