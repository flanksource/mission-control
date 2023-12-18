package db

import (
	"fmt"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/upstream"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func GetAllResourceIDsOfAgent(ctx context.Context, req upstream.PaginateRequest, agentID uuid.UUID) ([]string, error) {
	var response []string
	var err error

	switch req.Table {
	case "check_statuses":
		query := `
		SELECT (check_id::TEXT || ',' || time::TEXT) 
		FROM check_statuses 
		LEFT JOIN checks ON checks.id = check_statuses.check_id 
		WHERE checks.agent_id = ? AND (check_statuses.check_id::TEXT, check_statuses.time::TEXT) > (?, ?)
		ORDER BY check_statuses.check_id, check_statuses.time
		LIMIT ?`
		parts := strings.Split(req.From, ",")
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s is not a valid next cursor. It must consist of check_id and time separated by a comma", req.From)
		}

		err = ctx.DB().Raw(query, agentID, parts[0], parts[1], req.Size).Scan(&response).Error
	default:
		query := fmt.Sprintf("SELECT id FROM %s WHERE agent_id = ? AND id::TEXT > ? ORDER BY id LIMIT ?", req.Table)
		err = ctx.DB().Raw(query, agentID, req.From, req.Size).Scan(&response).Error
	}

	return response, err
}

func InsertUpstreamMsg(ctx context.Context, req *upstream.PushData) error {
	if len(req.Topologies) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Topologies).Error; err != nil {
			return fmt.Errorf("error upserting topologies: %w", err)
		}
	}

	if len(req.Canaries) > 0 {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(req.Canaries).Error; err != nil {
			return fmt.Errorf("error upserting canaries: %w", err)
		}
	}

	// components are inserted one by one, instead of in a batch, because of the foreign key constraint with itself.
	for _, c := range req.Components {
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true}).Create(&c).Error; err != nil {
			logger.Errorf("error upserting component (id=%s): %v", c.ID, err)
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
			logger.Errorf("error upserting config item (id=%s): %v", ci.ID, err)
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
		cols := []clause.Column{{Name: "canary_id"}, {Name: "type"}, {Name: "name"}, {Name: "agent_id"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).Create(req.Checks).Error; err != nil {
			return fmt.Errorf("error upserting checks: %w", err)
		}
	}

	if len(req.CheckStatuses) > 0 {
		cols := []clause.Column{{Name: "check_id"}, {Name: "time"}}
		if err := ctx.DB().Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.CheckStatuses, 1000).Error; err != nil {
			return fmt.Errorf("error upserting check_statuses: %w", err)
		}
	}

	return nil
}
