// Package controller exposes plugin operations and the plugin tab catalog
// over HTTP.
//
// Routes:
//
//	GET  /api/plugins?config_id=X
//	    Lists every running plugin whose ResourceSelector matches the
//	    given config item. Used by the frontend to populate the tab bar
//	    on the catalog detail page.
//
//	POST /api/plugins/:name/invoke/:op?config_id=X
//	    Invokes a plugin operation. The body is the operation's params
//	    (JSON). The response body is whatever the plugin returned via
//	    InvokeResponse.result, with the plugin's declared MIME type
//	    (typically application/clicky+json).
package gateway

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flanksource/clicky/rpc"
	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/plugin/machinery"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

// RegisterRoutes wires the plugin HTTP API onto the given echo instance.
func RegisterRoutes(e *echo.Echo) {
	g := e.Group("/api/plugins")
	g.GET("", ListPlugins, rbac.Authorization(policy.ObjectCatalog, policy.ActionRead))
	g.POST("/:name/invoke/:op", InvokeOperation)

	registerProxyRoutes(e)
}

// PluginListing is what GET /api/plugins returns: a flat list of plugins
// applicable to the current config item, with their tabs and operations.
type PluginListing struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Version     string              `json:"version,omitempty"`
	Agent       *models.Agent       `json:"agent,omitempty"`
	Tabs        []*api.TabSpec      `json:"tabs,omitempty"`
	Operations  []*api.OperationDef `json:"operations,omitempty"`
}

type ClickyRPCListing struct {
	Name    string         `json:"name"`
	Service rpc.RPCService `json:"service"`
}

// ListPlugins returns every running plugin whose CRD selector matches the
// (optional) config_id query parameter. With no config_id, returns every
// plugin (useful for global tabs).
func ListPlugins(c echo.Context) error {
	if c.QueryParam("format") == "clicky-rpc" {
		return listPluginsClickyRPC(c)
	}

	ctx := c.Request().Context().(dutyContext.Context)
	configID := c.QueryParam("config_id")
	out := []PluginListing{}
	for _, e := range plugin.DefaultRegistry.List() {
		if e.Manifest == nil {
			continue
		}

		if configID != "" {
			matches, err := machinery.SelectorMatches(ctx, e, configID)
			if err != nil {
				return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
			}
			if !matches {
				continue
			}
		}

		item := PluginListing{
			Name:        e.Name,
			Description: e.Manifest.Description,
			Version:     e.Manifest.Version,
			Tabs:        e.Manifest.Tabs,
			Operations:  e.Manifest.Operations,
		}

		if e.AgentID != nil {
			agent, err := query.FindCachedAgent(ctx, e.AgentID.String())
			if err != nil {
				return fmt.Errorf("failed to get agent for plugin %q: %w", e.Name, err)
			}

			item.Agent = agent
		}

		out = append(out, item)
	}

	return c.JSON(http.StatusOK, out)
}

func listPluginsClickyRPC(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	configID := c.QueryParam("config_id")
	out := []ClickyRPCListing{}
	for _, e := range plugin.DefaultRegistry.List() {
		if e.Manifest == nil {
			continue
		}
		if configID != "" {
			matches, err := machinery.SelectorMatches(ctx, e, configID)
			if err != nil {
				return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
			}
			if !matches {
				continue
			}
		}
		out = append(out, ClickyRPCListing{
			Name:    e.Name,
			Service: manifestcache.ManifestToService(e.Manifest),
		})
	}
	return c.JSON(http.StatusOK, out)
}

// InvokeOperation invokes a plugin operation. Local plugins are invoked through
// the in-process gRPC machinery; proxied plugins are forwarded to their owning
// agent over the plugin tunnel.
func InvokeOperation(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	op := c.Param("op")

	configID := c.QueryParam("config_id")
	if configID == "" {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("config_id is required"))
	}
	configUUID, err := uuid.Parse(configID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("config_id is invalid"))
	}

	entry, err := machinery.ResolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	switch entry.Kind {
	case api.PluginKindProxied:
		return invokeProxiedOperation(c, ctx, entry, pluginRef, op, configID, configUUID)
	case "", api.PluginKindLocal:
		resp, err := invokeLocalOperation(ctx, c.Request(), entry, pluginRef, op, configID, configUUID)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}
		mime := resp.Mime
		if mime == "" {
			mime = "application/json"
		}
		c.Response().Header().Set(echo.HeaderContentType, mime)
		return c.Blob(http.StatusOK, mime, resp.Result)
	default:
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("plugin %q has unsupported connection kind %q", pluginRef, entry.Kind))
	}
}

func invokeProxiedOperation(c echo.Context, ctx dutyContext.Context, entry *plugin.Entry, pluginRef, op, configID string, configUUID uuid.UUID) error {
	if machinery.OperationDef(entry, op) == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q operation %q not found", pluginRef, op))
	}

	matches, err := machinery.SelectorMatches(ctx, entry, configID)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	if !matches {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q is not enabled for config %s", pluginRef, configID))
	}

	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("not logged in"))
	}
	if err := machinery.EnforceInvokePermission(ctx, user.ID.String(), entry, op, configID); err != nil {
		return dutyAPI.WriteError(c, err)
	}
	roles, err := pluginRolesForUser(ctx, entry, configID)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	invocationToken, err := plugin.MintInvocationToken(user.ID.String(), entry.ID, 0, roles...)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "mint plugin invocation token"))
	}
	c.Request().Header.Set(api.InvocationTokenHTTPHeader, invocationToken)

	result, err := proxyToAgentPlugin(c, entry)
	if err != nil {
		recordPluginInvocation(ctx, entry, op, configUUID, "http", c.Request().Method, "", err.Error(), c.Request(), nil)
		return dutyAPI.WriteError(c, err)
	}
	recordPluginInvocation(ctx, entry, op, configUUID, "http", c.Request().Method, "", result.ErrorMessage, c.Request(), nil)
	return nil
}

func invokeLocalOperation(ctx dutyContext.Context, req *http.Request, entry *plugin.Entry, pluginRef, op, configID string, configUUID uuid.UUID) (*api.InvokeResponse, error) {
	var roles []string
	var subject string
	invocationToken := req.Header.Get(api.InvocationTokenHTTPHeader)
	if invocationToken == "" {
		var err error
		roles, err = pluginRolesForUser(ctx, entry, configID)
		if err != nil {
			return nil, err
		}

		if ctx.User() == nil {
			return nil, ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("cannot invoke local operation")
		}
		subject = ctx.User().ID.String()
	}
	return invokeLocalOperationWithRoles(ctx, req, entry, pluginRef, op, configID, configUUID, roles, subject, invocationToken)
}

func invokeLocalOperationWithRoles(ctx dutyContext.Context, req *http.Request, entry *plugin.Entry, pluginRef, op, configID string, configUUID uuid.UUID, roles []string, subject string, invocationToken string) (*api.InvokeResponse, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "read request body")
	}

	paramsHash := hashBytes(body)
	resp, invokedEntry, err := machinery.InvokeOperation(ctx, machinery.Request{
		Context:         req.Context(),
		PluginRef:       pluginRef,
		Operation:       op,
		ConfigItemID:    configID,
		ParamsJSON:      body,
		Subject:         subject,
		Roles:           roles,
		Depth:           0,
		Timeout:         60 * time.Second,
		InvocationToken: invocationToken,
	})
	if invokedEntry != nil {
		entry = invokedEntry
	}
	if err != nil {
		if entry != nil {
			recordPluginInvocation(ctx, entry, op, configUUID, "grpc", req.Method, paramsHash, err.Error(), req, body)
		}
		return nil, err
	}
	if resp.ErrorMessage != "" {
		recordPluginInvocation(ctx, entry, op, configUUID, "grpc", req.Method, paramsHash, resp.ErrorMessage, req, body)
		return nil, ctx.Oops().Code(resp.ErrorCode).Errorf("%s", resp.ErrorMessage)
	}

	recordPluginInvocation(ctx, entry, op, configUUID, "grpc", req.Method, paramsHash, "", req, body)
	return resp, nil
}
