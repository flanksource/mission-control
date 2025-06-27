package shorturl

import (
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"

	echoSrv "github.com/flanksource/incident-commander/echo"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /redirect routes")
	e.GET("/redirect/:alias", Redirect)
}

func Redirect(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	alias := c.Param("alias")

	if alias == "" {
		return api.WriteError(c, api.Errorf(api.EINVALID, "alias is required"))
	}

	shortener, err := Get(ctx, alias)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.Redirect(http.StatusFound, shortener.URL)
}
