package upstream

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
)

func UpstreamPushesCtrl(c echo.Context) error {
	var req api.ConfigChanges
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "invalid json request",
		})
	}

	if err := insertUpstreamMsg(c.Request().Context(), &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "something went wrong",
		})
	}

	logger.Infof("Checked at %v", req.CheckedAt)
	return nil
}

func insertUpstreamMsg(ctx context.Context, req *api.ConfigChanges) error {
	// TODO: Only testing ....
	if err := db.Gorm.WithContext(ctx).Table("components").CreateInBatches(req.Components, 250).Error; err != nil {
		return err
	}

	return nil
}
