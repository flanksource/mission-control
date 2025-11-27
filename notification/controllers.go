package notification

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
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

	g.POST("/silence", handleCreateSilence, rbac.Authorization(policy.ObjectNotification, policy.ActionCreate))

	g.GET("/silence_preview", NotificationSilencePreview, rbac.Authorization(policy.ObjectNotification, policy.ActionRead))
}

func handleCreateSilence(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req SilenceSaveRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid post body: %v", err))
	}

	req.Source = models.SourceUI
	if err := SaveNotificationSilence(ctx, req); err != nil {
		return api.WriteError(c, err)
	}

	return nil
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

type NotificationSilencePreviewItem struct {
	models.NotificationSendHistory `json:",inline"`
	Resource                       map[string]any `json:"resource"`
}

func NotificationSilencePreview(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	params := CanSilenceParams{
		ResourceID:   c.QueryParam("id"),
		ResourceType: c.QueryParam("type"),
		Recursive:    c.QueryParam("recursive") == "true",
		Filter:       c.QueryParam("filter"),
	}

	if selectorsRaw := c.QueryParam("selectors"); selectorsRaw != "" {
		var selectors types.ResourceSelectors
		if err := json.Unmarshal([]byte(selectorsRaw), &selectors); err != nil {
			return api.WriteError(c, api.Errorf(api.EINVALID, "invalid selectors: %v", err))
		}

		params.Selectors = selectors
	}

	h, err := db.GetNotificationSendHistory(ctx, 15, []string{models.NotificationStatusSent}, -1, -1)
	if err != nil {
		return api.WriteError(c, err)
	}
	silenced, err := CanSilence(ctx, h, params)
	if err != nil {
		return api.WriteError(c, err)
	}

	var resp []NotificationSilencePreviewItem
	for _, s := range silenced {
		rMap, err := GetResourceAsMapFromEvent(ctx, s.SourceEvent, s.ResourceID.String())
		if err != nil {
			return api.WriteError(c, err)
		}
		resp = append(resp, NotificationSilencePreviewItem{
			NotificationSendHistory: s,
			Resource:                rMap,
		})
	}

	return c.JSON(200, resp)
}
