package notification

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

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
	g.GET("/send_history/:id", GetNotificationSendHistoryDetail, echoSrv.RLSMiddleware)

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

func GetNotificationSendHistoryDetail(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid notification history id: %s", id))
	}

	var detail NotificationSendHistoryDetail
	if err := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", id).First(&detail.NotificationSendHistory).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return api.WriteError(c, api.Errorf(api.ENOTFOUND, "notification history %s not found", id))
		}
		return api.WriteError(c, ctx.Oops().Wrap(err))
	}

	resourceKind := strings.Split(detail.SourceEvent, ".")[0]
	detail.ResourceKind = resourceKind
	if resourceMap, err := GetResourceAsMapFromEvent(ctx, detail.SourceEvent, detail.ResourceID.String()); err == nil && resourceMap != nil {
		if b, err := json.Marshal(resourceMap); err == nil {
			detail.Resource = types.JSON(b)
		}
		if resourceType, ok := resourceMap["type"].(string); ok && resourceType != "" {
			detail.ResourceType = &resourceType
		}
	}

	if len(detail.BodyPayload) > 0 {
		var payload NotificationMessagePayload
		if err := json.Unmarshal(detail.BodyPayload, &payload); err != nil {
			return api.WriteError(c, ctx.Oops().Wrapf(err, "failed to parse body payload"))
		}

		bodyMarkdown, err := FormatNotificationMessage(payload, "markdown")
		if err != nil {
			return api.WriteError(c, ctx.Oops().Wrapf(err, "failed to render body payload"))
		}

		detail.BodyMarkdown = bodyMarkdown
	}

	return c.JSON(http.StatusOK, detail)
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
