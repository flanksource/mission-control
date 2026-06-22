package gateway

import (
	"net/http"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/plugin"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// RegisterProxiedPlugin registers or updates one plugin reported by an authenticated agent.
func RegisterProxiedPlugin(c echo.Context) error {
	ctx := c.Request().Context().(dutyContext.Context)
	agent := ctx.Agent()
	if agent == nil || agent.ID == uuid.Nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("authenticated agent is required"))
	}

	var req plugin.PluginRegisterRequest
	if err := c.Bind(&req); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Wrapf(err, "invalid plugin register request"))
	}
	if req.ID == uuid.Nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("plugin id is required"))
	}
	if req.Name == "" {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("plugin name is required"))
	}
	if req.Manifest == nil {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("plugin manifest is required"))
	}

	if _, err := plugin.DefaultRegistry.UpsertProxied(req.ID, req.Namespace, req.Name, req.Spec, req.Manifest, agent.ID); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "upsert proxied plugin %s/%s", req.Namespace, req.Name))
	}

	return c.NoContent(http.StatusCreated)
}
