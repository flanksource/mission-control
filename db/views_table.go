package db

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
)

func InsertPanelResults(ctx context.Context, viewID uuid.UUID, panels []api.PanelResult) error {
	results, err := json.Marshal(panels)
	if err != nil {
		return fmt.Errorf("failed to marshal panel results: %w", err)
	}

	record := models.ViewPanel{
		ViewID:  viewID,
		Results: results,
	}

	if err := ctx.DB().Save(&record).Error; err != nil {
		return fmt.Errorf("failed to save panel results: %w", err)
	}

	return nil
}
