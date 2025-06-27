package shorturl

import (
	"net/http"
	"net/url"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	echoSrv "github.com/flanksource/incident-commander/echo"
)

const redirectPath = "/redirect"

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /redirect routes")
	e.GET(redirectPath+"/:alias", Redirect)
}

func Redirect(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	alias := c.Param("alias")

	if alias == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "alias is required"))
	}

	targetURL, err := Get(ctx, alias)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.Redirect(http.StatusFound, targetURL)
}

func FullShortURL(alias string) string {
	f, _ := url.JoinPath(api.FrontendURL, redirectPath, alias)
	return f
}
