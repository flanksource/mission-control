package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/plugin/machinery"
	"github.com/flanksource/incident-commander/upstream/tunnel"
)

func registerProxyRoutes(e *echo.Echo) {
	g := e.Group("/api/plugins")
	// Plugin UI assets are public for registered plugins so a whitelisted plugin
	// can be embedded in a cross-site iframe where the session cookie is not sent.
	// uiProxy resolves the plugin and 404s unregistered ones; the bundle carries
	// no user data. Operation calls remain authenticated by the invocation token.
	g.GET("/:name/ui", uiProxy)
	g.GET("/:name/ui/*", uiProxy)
	g.GET("/:name/ui-token", pluginUIToken)
	g.Any("/:name/proxy/:op", operationHTTPProxy)
}

// pluginUIToken mints a short-lived, plugin-scoped invocation token for a
// whitelisted plugin's UI to authenticate its operation calls. The parent app
// calls this same-origin (with the user's session), then hands the token to the
// cross-site iframe, which sends it on operation requests. The token carries only
// the roles the user actually holds for this plugin and config.
func pluginUIToken(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	configID := c.QueryParam("config_id")
	if configID == "" {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("config_id is required"))
	}

	entry, err := machinery.ResolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
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

	roles, err := pluginRolesForUser(ctx, entry, configID)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	token, err := plugin.MintInvocationToken(user.ID.String(), entry.ID, 0, roles...)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "mint plugin invocation token"))
	}

	// The response carries a bearer token; never let a browser or proxy cache it.
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, map[string]any{
		"token":            token,
		"expiresInSeconds": int(plugin.PluginJWTTTL.Seconds()),
	})
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

	if entry.Kind == api.PluginKindProxied {
		if _, err := proxyToAgentPlugin(c, entry); err != nil {
			return dutyAPI.WriteError(c, err)
		}
		return nil
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

	paramsHash := httpParamsHash(c.Request().Method, c.QueryParams())

	var roles []string
	var subject string
	invocationToken := c.Request().Header.Get(api.InvocationTokenHTTPHeader)
	if invocationToken != "" {
		// Proxied operations arriving on an agent already carry an upstream-minted
		// invocation token. Validate and reuse it rather than minting an agent-signed token.
		claims, err := plugin.ValidateRequestInvocationToken(c.Request().Context(), invocationToken, entry.ID)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("invalid plugin invocation token: %v", err))
		}

		subject = claims.Subject
		roles = claims.Roles

		// A UI-minted token (local/remote plugins) is issued without an
		// operation-level check, so authorize the operation here against the
		// token's subject. Proxied tokens were already authorized upstream and
		// this agent lacks the RBAC data to re-check.
		if entry.Kind != api.PluginKindProxied {
			if err := machinery.EnforceInvokePermission(ctx, subject, entry, op, configID); err != nil {
				return dutyAPI.WriteError(c, err)
			}
		}
	} else {
		// No invocation token was supplied, so authorize locally before minting one.
		user := ctx.User()
		if user == nil {
			return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("not logged in"))
		}

		subject = user.ID.String()
		if err := machinery.EnforceInvokePermission(ctx, subject, entry, op, configID); err != nil {
			return dutyAPI.WriteError(c, err)
		}

		var err error
		roles, err = pluginRolesForUser(ctx, entry, configID)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}
	}

	if entry.Kind == api.PluginKindProxied {
		invocationToken, err = plugin.MintInvocationToken(subject, entry.ID, 0, roles...)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "mint plugin invocation token"))
		}

		c.Request().Header.Set(api.InvocationTokenHTTPHeader, invocationToken)
		result, err := proxyToAgentPlugin(c, entry)
		if err != nil {
			recordPluginInvocation(ctx, entry, op, configUUID, "http", c.Request().Method, paramsHash, err.Error(), c.Request(), nil)
			return dutyAPI.WriteError(c, err)
		}

		recordPluginInvocation(ctx, entry, op, configUUID, "http", c.Request().Method, paramsHash, result.ErrorMessage, c.Request(), nil)
		return nil
	}

	if invocationToken == "" {
		invocationToken, err = plugin.MintInvocationToken(subject, entry.ID, 0, roles...)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "mint plugin invocation token"))
		}
	}

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

type agentPluginProxyResult struct {
	StatusCode   int
	ErrorMessage string
}

func proxyToAgentPlugin(c echo.Context, entry *plugin.Entry) (agentPluginProxyResult, error) {
	agentID, err := proxiedPluginAgentID(c, entry)
	if err != nil {
		return agentPluginProxyResult{}, err
	}

	result := agentPluginProxyResult{}
	target := &url.URL{Scheme: "http", Host: "agent.local"}
	rp := &httputil.ReverseProxy{
		Transport:     tunnel.NewTransport(agentID),
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			result.StatusCode = resp.StatusCode
			if resp.StatusCode >= http.StatusBadRequest {
				result.ErrorMessage = fmt.Sprintf("agent plugin proxy returned HTTP %d", resp.StatusCode)
			}
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			status := http.StatusBadGateway
			if errors.Is(err, tunnel.ErrSessionClosed) {
				status = http.StatusServiceUnavailable
			}
			result.StatusCode = status
			result.ErrorMessage = fmt.Sprintf("agent plugin proxy failed: %v", err)
			if !c.Response().Committed {
				http.Error(rw, http.StatusText(status), status)
			}
		},
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = pr.In.URL.Path
			pr.Out.URL.RawPath = pr.In.URL.RawPath
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.Header.Del(echo.HeaderAuthorization)
			pr.Out.Header.Del(echo.HeaderCookie)
			pr.Out.Header.Del("Proxy-Authorization")
			if token := pr.In.Header.Get(api.InvocationTokenHTTPHeader); token != "" {
				pr.Out.Header.Set(api.InvocationTokenHTTPHeader, token)
			}
		},
	}
	rp.ServeHTTP(c.Response().Writer, c.Request())
	return result, nil
}

func proxiedPluginAgentID(c echo.Context, entry *plugin.Entry) (uuid.UUID, error) {
	ctx := c.Request().Context().(dutyContext.Context)
	if entry.AgentID == nil || *entry.AgentID == uuid.Nil {
		return uuid.Nil, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("proxied plugin %q is not assigned to an agent", entry.Name)
	}
	return *entry.AgentID, nil
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
