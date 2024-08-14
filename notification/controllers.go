package notification

import (
	"net/http"

	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/labstack/echo/v4"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	e.GET("/notification/events", func(c echo.Context) error {
		return c.JSON(http.StatusOK, EventRing.Get())
	}, rbac.Authorization(rbac.ObjectMonitor, rbac.ActionRead))
}
