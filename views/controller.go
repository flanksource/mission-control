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
	"github.com/flanksource/incident-commander/utils"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /views routes")

	g := e.Group("/view", rbac.Authorization(policy.ObjectCatalog, policy.ActionRead))
	g.GET("/:namespace/:name", GetView)
}

func GetView(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	cacheControl := c.Request().Header.Get("Cache-Control")
	headerMaxAge, headerRefreshTimeout, err := utils.ParseCacheControlHeader(cacheControl)
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid cache control header: %s", err.Error()))
	}

	var opts []ViewOption
	if headerMaxAge > 0 {
		opts = append(opts, WithMaxAge(headerMaxAge))
	}
	if headerRefreshTimeout > 0 {
		opts = append(opts, WithRefreshTimeout(headerRefreshTimeout))
	}

	response, err := ReadOrPopulateViewTable(ctx, namespace, name, opts...)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}
