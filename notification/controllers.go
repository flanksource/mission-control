package notification

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/labstack/echo/v4"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	g := e.Group("/notification")

	g.GET("/events", func(c echo.Context) error {
		return c.JSON(http.StatusOK, EventRing.Get())
	}, rbac.Authorization(rbac.ObjectMonitor, rbac.ActionRead))

	g.POST("/silence", func(c echo.Context) error {
		ctx := c.Request().Context().(context.Context)

		var req SilenceSaveRequest
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
			return err
		}

		req.Source = models.SourceUI
		if err := SaveNotificationSilence(ctx, req); err != nil {
			return api.WriteError(c, err)
		}

		return nil
	}, rbac.Authorization(rbac.ObjectNotification, rbac.ActionCreate))
}
