package views

import (
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /views routes")

	g := e.Group("/views", rbac.Authorization(policy.ObjectCatalog, policy.ActionRead))
	g.GET("/:namespace/:name", ViewRun)
}

func ViewRun(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	response, err := runView(ctx, namespace, name)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func runView(ctx context.Context, namespace, name string) (*api.ViewResult, error) {
	view, err := db.GetView(ctx, namespace, name)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to find view %s/%s: %w", namespace, name, err)
	}
	if view == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("view %s/%s not found", namespace, name)
	}

	response, err := Run(ctx, view)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to run view %s/%s: %w", namespace, name, err)
	}

	response.Columns = view.Spec.Columns
	return response, nil
}
