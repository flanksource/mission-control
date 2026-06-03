package gateway

import (
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
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/plugin/machinery"
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
	entry, err := machinery.ResolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if cfg := c.QueryParam("config_id"); cfg != "" {
		matches, err := machinery.SelectorMatches(ctx, entry, cfg)
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

	return proxyToPluginUI(c, entry, prefix)
}

func operationHTTPProxy(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	op := c.Param("op")

	entry, err := machinery.ResolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	def := machinery.OperationDef(entry, op)
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
	matches, err := machinery.SelectorMatches(ctx, entry, configID)
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
	if err := machinery.EnforceInvokePermission(ctx, subject, entry, op, configID); err != nil {
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
	if err := proxyToPluginOperation(c, entry, op, invocationToken); err != nil {
		return err
	}

	recordPluginInvocation(ctx, entry, op, configUUID, "http", c.Request().Method, paramsHash, "", c.Request(), nil)

	return nil
}

func proxyToPluginUI(c echo.Context, entry *plugin.Entry, prefix string) error {
	target, err := pluginHTTPURL(c, entry)
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

func proxyToPluginOperation(c echo.Context, entry *plugin.Entry, op, invocationToken string) error {
	target, err := pluginHTTPURL(c, entry)
	if err != nil {
		return err
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = pluginOperationTargetPath(op)
			pr.Out.Header.Set(api.InvocationTokenHTTPHeader, invocationToken)
			pr.Out.URL.RawPath = ""
		},
	}

	rp.ServeHTTP(c.Response().Writer, c.Request())
	return nil
}

func pluginHTTPURL(c echo.Context, entry *plugin.Entry) (*url.URL, error) {
	ctx := c.Request().Context().(dutyContext.Context)
	target, err := machinery.HTTPURL(ctx, entry.ID)
	if err != nil {
		return nil, dutyAPI.WriteError(c, err)
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

func operationHTTPBindingAllowed(def *api.OperationDef, method string) bool {
	for _, binding := range def.Http {
		if binding != nil && strings.EqualFold(binding.Method, method) {
			return true
		}
	}
	return false
}

func allowedUIPath(entry *plugin.Entry, p string) bool {
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
