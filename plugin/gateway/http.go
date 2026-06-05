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
	"io"
	"net/http"
	"time"

	"github.com/flanksource/clicky/rpc"
	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	echoSrv "github.com/flanksource/incident-commander/echo"
	plugin "github.com/flanksource/incident-commander/plugin"
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
		out = append(out, PluginListing{
			Name:        e.Name,
			Description: e.Manifest.Description,
			Version:     e.Manifest.Version,
			Tabs:        e.Manifest.Tabs,
			Operations:  e.Manifest.Operations,
		})
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

// InvokeOperation proxies a request to the plugin's gRPC Invoke endpoint.
// The plugin returns raw bytes plus a MIME type; we forward both verbatim.
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
	roles, err := pluginRolesForUser(ctx, entry, configID)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "read request body"))
	}

	paramsHash := hashBytes(body)
	resp, entry, err := machinery.InvokeOperation(ctx, machinery.Request{
		Context:      c.Request().Context(),
		PluginRef:    pluginRef,
		Operation:    op,
		ConfigItemID: configID,
		ParamsJSON:   body,
		User:         ctx.User(),
		Roles:        roles,
		Depth:        0,
		Timeout:      60 * time.Second,
	})
	if err != nil {
		if entry != nil {
			recordPluginInvocation(ctx, entry, op, configUUID, "grpc", c.Request().Method, paramsHash, err.Error(), c.Request(), body)
		}
		return dutyAPI.WriteError(c, err)
	}
	if resp.ErrorMessage != "" {
		recordPluginInvocation(ctx, entry, op, configUUID, "grpc", c.Request().Method, paramsHash, resp.ErrorMessage, c.Request(), body)
		return dutyAPI.WriteError(c, ctx.Oops().Code(resp.ErrorCode).Errorf("%s", resp.ErrorMessage))
	}

	recordPluginInvocation(ctx, entry, op, configUUID, "grpc", c.Request().Method, paramsHash, "", c.Request(), body)

	mime := resp.Mime
	if mime == "" {
		mime = "application/json"
	}
	c.Response().Header().Set(echo.HeaderContentType, mime)
	return c.Blob(http.StatusOK, mime, resp.Result)
}
