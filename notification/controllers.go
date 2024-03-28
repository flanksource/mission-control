package notification

import (
	"net/http"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/postq"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func RegisterRoutes(e *echo.Echo) {
	apiGroup := e.Group("/notifications")
	apiGroup.POST("/test", TestNotification)
}

func TestNotification(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var reqData struct {
		ID        uuid.UUID `json:"id"`
		EventName string    `json:"eventName"`
	}
	if err := c.Bind(&reqData); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request: %v", err))
	}

	e := postq.Event{
		Name:       reqData.EventName,
		Properties: map[string]string{"id": reqData.ID.String()},
		CreatedAt:  time.Now(),
	}

	if err := addNotificationEvent(ctx, e); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINTERNAL, "unable to create notification event: %v", err))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}
