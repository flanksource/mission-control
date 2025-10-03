package notification

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/labstack/echo/v4"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	g := e.Group("/notification")

	g.POST("/summary", NotificationSendHistorySummary, echoSrv.RLSMiddleware)

	g.GET("/events", func(c echo.Context) error {
		return c.JSON(http.StatusOK, EventRing.Get())
	}, rbac.Authorization(policy.ObjectMonitor, policy.ActionRead))

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
	}, rbac.Authorization(policy.ObjectNotification, policy.ActionCreate))

	g.GET("/silence_preview", NotificationSilencePreview)
}

func NotificationSendHistorySummary(c echo.Context) error {
	var req query.NotificationSendHistorySummaryRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	ctx := c.Request().Context().(context.Context)

	response, err := query.NotificationSendHistorySummary(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func NotificationSilencePreview(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	// Get all the notifications sent in past 15 days
	h, err := db.GetNotificationSendHistory(ctx, 15, -1, -1)
	if err != nil {
		return api.WriteError(c, err)
	}
	var silenced []models.NotificationSendHistory
	var err2 error
	if resourceID := c.QueryParam("id"); resourceID != "" {
		silenced = CanSilenceViaResourceID(h, resourceID)
	}
	if filter := c.QueryParam("filter"); filter != "" {
		silenced, err2 = CanSilenceViaFilter(ctx, h, filter)
	}
	if selectorsRaw := c.QueryParam("selectors"); selectorsRaw != "" {
		var selectors types.ResourceSelectors
		if err := json.Unmarshal([]byte(selectorsRaw), &selectors); err != nil {
			return api.WriteError(c, err)
		}
		silenced, err2 = CanSilenceViaSelectors(ctx, h, selectors)
	}
	if err2 != nil {
		return api.WriteError(c, err2)
	}
	return c.JSON(200, silenced)
}
