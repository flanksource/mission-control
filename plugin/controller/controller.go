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
//	POST /api/plugins/:name/operations/:op?config_id=X
//	    Invokes a plugin operation. The body is the operation's params
//	    (JSON). The response body is whatever the plugin returned via
//	    InvokeResponse.result, with the plugin's declared MIME type
//	    (typically application/clicky+json).
package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
	"google.golang.org/grpc/metadata"

	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
	"github.com/flanksource/incident-commander/plugin/supervisor"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

// registerUIProxy is set by proxy.go's init so the proxy registration code
// can stay in its own file without an import cycle.
var registerUIProxy func(e *echo.Echo)

// RegisterRoutes wires the plugin HTTP API onto the given echo instance.
func RegisterRoutes(e *echo.Echo) {
	g := e.Group("/api/plugins")
	g.GET("", ListPlugins, rbac.Authorization(policy.ObjectCatalog, policy.ActionRead))
	g.POST("/:name/operations/:op", InvokeOperation, rbac.Authorization(policy.ObjectCatalog, policy.ActionUpdate))

	if registerUIProxy != nil {
		registerUIProxy(e)
	}
}

// PluginListing is what GET /api/plugins returns: a flat list of plugins
// applicable to the current config item, with their tabs and operations.
type PluginListing struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	Version     string                   `json:"version,omitempty"`
	Tabs        []*pluginpb.TabSpec      `json:"tabs,omitempty"`
	Operations  []*pluginpb.OperationDef `json:"operations,omitempty"`
}

// ListPlugins returns every running plugin whose CRD selector matches the
// (optional) config_id query parameter. With no config_id, returns every
// plugin (useful for global tabs).
func ListPlugins(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	configID := c.QueryParam("config_id")
	out := []PluginListing{}
	for _, e := range registry.Default.List() {
		if e.Manifest == nil {
			continue
		}
		if configID != "" && !selectorMatches(ctx, e, configID) {
			continue
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

// InvokeOperation proxies a request to the plugin's gRPC Invoke endpoint.
// The plugin returns raw bytes plus a MIME type; we forward both verbatim.
func InvokeOperation(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	pluginRef := c.Param("name")
	op := c.Param("op")

	entry, err := resolvePlugin(ctx, pluginRef)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	sup := supervisor.LookupSupervisor(entry.ID)
	if sup == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q not running", pluginRef))
	}

	configID := c.QueryParam("config_id")
	if configID != "" && !selectorMatches(ctx, entry, configID) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q is not enabled for config %s", pluginRef, configID))
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "read request body"))
	}

	invocationToken, err := auth.MintPluginInvocationToken(ctx.User(), entry.ID.String())
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "mint plugin invocation token"))
	}

	caller := callerFromCtx(ctx)
	invokeCtx, cancel := context.WithTimeout(c.Request().Context(), 60*time.Second)
	defer cancel()

	// Like Set-Cookie in HTTP, Mission Control issues a short-lived invocation
	// token to the plugin over gRPC metadata. The plugin SDK presents the same
	// token on HostService callbacks so Mission Control can verify the actor.
	invokeCtx = metadata.AppendToOutgoingContext(invokeCtx, pluginpb.PluginInvocationTokenMetadataKey, invocationToken)

	resp, err := sup.Invoke(invokeCtx, &pluginpb.InvokeRequest{
		Operation:    op,
		ParamsJson:   body,
		ConfigItemId: configID,
		Caller:       caller,
	})
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "plugin %s/%s", pluginRef, op))
	}
	if resp.ErrorMessage != "" {
		return dutyAPI.WriteError(c, ctx.Oops().Code(resp.ErrorCode).Errorf("%s", resp.ErrorMessage))
	}

	mime := resp.Mime
	if mime == "" {
		mime = "application/json"
	}
	c.Response().Header().Set(echo.HeaderContentType, mime)
	return c.Blob(http.StatusOK, mime, resp.Result)
}

func resolvePlugin(ctx dutyContext.Context, ref string) (*registry.Entry, error) {
	entry, err := registry.Default.Resolve(ref)
	if err != nil {
		if errors.Is(err, registry.ErrAmbiguousPlugin) {
			return nil, ctx.Oops().Code(dutyAPI.ECONFLICT).Wrap(err)
		}
		return nil, ctx.Oops().Wrap(err)
	}
	if entry == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q not running", ref)
	}
	return entry, nil
}

// selectorMatches returns true when the given config id satisfies the plugin
// CRD's ResourceSelector. Centralised so all routes apply the same check.
func selectorMatches(ctx dutyContext.Context, entry *registry.Entry, configID string) bool {
	selector := entry.Spec.Selector
	if selector.IsEmpty() {
		return true
	}

	item, err := query.ConfigItemFromCache(ctx, configID)
	if err != nil {
		return false
	}

	return selector.Matches(item)
}

func callerFromCtx(ctx dutyContext.Context) *pluginpb.CallerContext {
	cc := &pluginpb.CallerContext{}
	if u := ctx.User(); u != nil {
		cc.UserId = u.ID.String()
	}

	return cc
}
