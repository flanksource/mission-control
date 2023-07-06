package db

import (
	"errors"
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

var errTableReconcileNotAllowed = errors.New("table is not allowed to be reconciled")

func GetIDsHash(ctx *api.Context, table string, from uuid.UUID, size int) (*api.PaginateResponse, error) {
	if !collections.Contains(api.TablesToReconcile, table) {
		return nil, errTableReconcileNotAllowed
	}

	query := fmt.Sprintf(`
		WITH id_list AS (
			SELECT
				id::TEXT
			FROM %s
			WHERE id > ?
			ORDER BY id
			LIMIT ?
		)
		SELECT
			encode(digest(string_agg(id::TEXT, ''), 'sha256'), 'hex') as sha256sum,
			MAX(id) as last_id,
			COUNT(*) as total
		FROM
			id_list`, table)

	var resp api.PaginateResponse
	err := ctx.DB().Raw(query, from, size).Scan(&resp).Error
	return &resp, err
}

func GetMissingResourceIDs(ctx *api.Context, ids []string, paginateReq api.PaginateRequest) (*api.PushData, error) {
	var pushData api.PushData

	switch paginateReq.Table {
	case "canaries":
		if err := ctx.DB().Not(ids).Where("id > ?", paginateReq.From).Limit(paginateReq.Size).Order("id").Find(&pushData.Canaries).Error; err != nil {
			return nil, fmt.Errorf("error fetching canaries: %w", err)
		}

	case "checks":
		if err := ctx.DB().Not(ids).Where("id > ?", paginateReq.From).Limit(paginateReq.Size).Order("id").Find(&pushData.Checks).Error; err != nil {
			return nil, fmt.Errorf("error fetching checks: %w", err)
		}

	case "components":
		if err := ctx.DB().Not(ids).Where("id > ?", paginateReq.From).Limit(paginateReq.Size).Order("id").Find(&pushData.Components).Error; err != nil {
			return nil, fmt.Errorf("error fetching components: %w", err)
		}

	case "config_scrapers":
		if err := ctx.DB().Not(ids).Where("id > ?", paginateReq.From).Limit(paginateReq.Size).Order("id").Find(&pushData.ConfigScrapers).Error; err != nil {
			return nil, fmt.Errorf("error fetching config scrapers: %w", err)
		}

	case "config_items":
		if err := ctx.DB().Not(ids).Where("id > ?", paginateReq.From).Limit(paginateReq.Size).Order("id").Find(&pushData.ConfigItems).Error; err != nil {
			return nil, fmt.Errorf("error fetching config items: %w", err)
		}
	}

	return &pushData, nil
}

func GetAllResourceIDsOfAgent(ctx *api.Context, agentID string, req api.PaginateRequest) ([]string, error) {
	if !collections.Contains(api.TablesToReconcile, req.Table) {
		return nil, errTableReconcileNotAllowed
	}

	var response []string
	query := fmt.Sprintf("SELECT id FROM %s WHERE agent_id = ? AND id > ? ORDER BY id LIMIT ?", req.Table)
	err := ctx.DB().Raw(query, agentID, req.From, req.Size).Scan(&response).Error
	return response, err
}

func InsertUpstreamMsg(ctx *api.Context, req *api.PushData) error {
	if len(req.Canaries) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Canaries).Error; err != nil {
			return fmt.Errorf("error upserting canaries: %w", err)
		}
	}

	// components are inserted one by one, instead of in a batch, because of the foreign key constraint with itself.
	for _, c := range req.Components {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(&c).Error; err != nil {
			logger.Errorf("error upserting component (id=%s): %w", c.ID, err)
		}
	}

	if len(req.ComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "relationship_id"}, {Name: "selector_id"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ComponentRelationships).Error; err != nil {
			return fmt.Errorf("error upserting component_relationships: %w", err)
		}
	}

	if len(req.ConfigScrapers) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigScrapers).Error; err != nil {
			return fmt.Errorf("error upserting config scrapers: %w", err)
		}
	}

	// config items are inserted one by one, instead of in a batch, because of the foreign key constraint with itself.
	for _, ci := range req.ConfigItems {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(&ci).Error; err != nil {
			logger.Errorf("error upserting config item (id=%s): %w", ci.ID, err)
		}
	}

	if len(req.ConfigRelationships) > 0 {
		cols := []clause.Column{{Name: "related_id"}, {Name: "config_id"}, {Name: "selector_id"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ConfigRelationships).Error; err != nil {
			return fmt.Errorf("error upserting config_relationships: %w", err)
		}
	}

	if len(req.ConfigComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "config_id"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.ConfigComponentRelationships).Error; err != nil {
			return fmt.Errorf("error upserting config_component_relationships: %w", err)
		}
	}

	if len(req.ConfigChanges) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigChanges).Error; err != nil {
			return fmt.Errorf("error upserting config_changes: %w", err)
		}
	}

	if len(req.ConfigAnalysis) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.ConfigAnalysis).Error; err != nil {
			return fmt.Errorf("error upserting config_analysis: %w", err)
		}
	}

	if len(req.Checks) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Checks).Error; err != nil {
			return fmt.Errorf("error upserting checks: %w", err)
		}
	}

	if len(req.CheckStatuses) > 0 {
		cols := []clause.Column{{Name: "check_id"}, {Name: "time"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.CheckStatuses).Error; err != nil {
			return fmt.Errorf("error upserting check_statuses: %w", err)
		}
	}

	return nil
}
