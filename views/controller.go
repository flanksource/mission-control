package views

import (
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

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

	response, err := ReadOrPopulateViewTable(ctx, namespace, name)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}
