package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm/clause"
)

func UpstreamPushesCtrl(c echo.Context) error {
	var req api.PushData
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

func insertUpstreamMsg(ctx context.Context, req *api.PushData) error {
	if len(req.Components) > 0 {
		if err := db.Gorm.Table("components").Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Components).Error; err != nil {
			return fmt.Errorf("error upserting components; %w", err)
		}
	}

	if len(req.ConfigItems) > 0 {
		if err := db.Gorm.Table("config_items").Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigItems).Error; err != nil {
			return fmt.Errorf("error upserting config_items; %w", err)
		}
	}

	if len(req.ConfigRelationships) > 0 {
		cols := []clause.Column{{Name: "related_id"}, {Name: "config_id"}, {Name: "selector_id"}}
		if err := db.Gorm.Table("config_relationships").Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ConfigRelationships).Error; err != nil {
			return fmt.Errorf("error upserting config_relationships; %w", err)
		}
	}

	if len(req.ConfigChanges) > 0 {
		if err := db.Gorm.Table("config_changes").Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigChanges).Error; err != nil {
			return fmt.Errorf("error upserting config_changes; %w", err)
		}
	}

	if len(req.ConfigAnalysis) > 0 {
		if err := db.Gorm.Table("config_analysis").Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigAnalysis).Error; err != nil {
			return fmt.Errorf("error upserting config_analysis; %w", err)
		}
	}

	return nil
}
