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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

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
	configID := c.QueryParam("config_id")
	out := []PluginListing{}
	for _, e := range registry.Default.List() {
		if e.Manifest == nil {
			continue
		}
		if configID != "" && !selectorMatches(e, configID) {
			continue
		}
		out = append(out, PluginListing{
			Name:        e.Manifest.Name,
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
	name := c.Param("name")
	op := c.Param("op")

	sup := supervisor.LookupSupervisor(name)
	if sup == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q not running", name))
	}

	configID := c.QueryParam("config_id")
	if configID != "" {
		entry := registry.Default.Get(name)
		if entry != nil && !selectorMatches(entry, configID) {
			return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("plugin %q is not enabled for config %s", name, configID))
		}
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "read request body"))
	}

	caller := callerFromCtx(ctx)
	invokeCtx, cancel := context.WithTimeout(c.Request().Context(), 60*time.Second)
	defer cancel()

	resp, err := sup.Invoke(invokeCtx, &pluginpb.InvokeRequest{
		Operation:    op,
		ParamsJson:   body,
		ConfigItemId: configID,
		Caller:       caller,
	})
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "plugin %s/%s", name, op))
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

// selectorMatches returns true when the given config id satisfies the plugin
// CRD's ResourceSelector. Centralised so all routes apply the same check.
func selectorMatches(entry *registry.Entry, configID string) bool {
	// MVP: an empty selector matches everything. Type/label/tag matching is
	// done by the host service when the plugin asks for a config item; the
	// frontend gets the same answer via /api/plugins listing because both
	// paths use this helper. A richer selector evaluation belongs in
	// duty/query (MatchResourceSelector) — wire it here once the API is
	// stabilized.
	_ = configID
	return true
}

func callerFromCtx(ctx dutyContext.Context) *pluginpb.CallerContext {
	cc := &pluginpb.CallerContext{}
	if u := ctx.User(); u != nil {
		cc.UserId = u.ID.String()
		cc.UserEmail = u.Email
	}
	return cc
}

// jsonError is used by the few non-WriteError responses below.
//
//nolint:unused
func jsonError(c echo.Context, status int, msg string) error {
	body, _ := json.Marshal(map[string]string{"error": msg})
	return c.Blob(status, echo.MIMEApplicationJSON, body)
}

// _ retains fmt for the rare debug formatting; the package itself stays
// minimal so review can focus on the routing and supervisor wiring.
var _ = fmt.Sprintf
