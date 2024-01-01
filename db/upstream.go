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
	batchSize := 100
	db := ctx.DB()
	if len(req.Topologies) > 0 {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.Topologies, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting topologies: %w", err)
		}
	}

	if len(req.Canaries) > 0 {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.Canaries, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting canaries: %w", err)
		}
	}

	// components are inserted one by one, instead of in a batch, because of the foreign key constraint with itself.
	for _, c := range req.Components {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.Components, batchSize).Error; err != nil {
			logger.Errorf("error upserting component (id=%s): %v", c.ID, err)
		}
	}

	if len(req.ComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "relationship_id"}, {Name: "selector_id"}}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.ComponentRelationships, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting component_relationships: %w", err)
		}
	}

	if len(req.ConfigScrapers) > 0 {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.ConfigScrapers, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting config scrapers: %w", err)
		}
	}

	// config items are inserted one by one, instead of in a batch, because of the foreign key constraint with itself.
	for _, ci := range req.ConfigItems {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(&ci, batchSize).Error; err != nil {
			logger.Errorf("error upserting config item (id=%s): %v", ci.ID, err)
		}
	}

	if len(req.ConfigRelationships) > 0 {
		cols := []clause.Column{{Name: "related_id"}, {Name: "config_id"}, {Name: "selector_id"}}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.ConfigRelationships, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting config_relationships: %w", err)
		}
	}

	if len(req.ConfigComponentRelationships) > 0 {
		cols := []clause.Column{{Name: "component_id"}, {Name: "config_id"}}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.ConfigComponentRelationships, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting config_component_relationships: %w", err)
		}
	}

	if len(req.ConfigChanges) > 0 {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.ConfigChanges, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting config_changes: %w", err)
		}
	}

	if len(req.ConfigAnalysis) > 0 {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.ConfigAnalysis, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting config_analysis: %w", err)
		}
	}

	if len(req.Checks) > 0 {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(req.Checks, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting checks: %w", err)
		}
	}

	if len(req.CheckStatuses) > 0 {
		cols := []clause.Column{{Name: "check_id"}, {Name: "time"}}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true, Columns: cols}).CreateInBatches(req.CheckStatuses, batchSize).Error; err != nil {
			return fmt.Errorf("error upserting check_statuses: %w", err)
		}
	}

	return nil
}
