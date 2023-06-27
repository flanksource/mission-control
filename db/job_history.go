package db

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
)

func PersistJobHistory(ctx *api.Context, h *models.JobHistory) error {
	if ctx.DB() == nil {
		return nil
	}

	// Delete jobs which did not process anything
	if h.ID != uuid.Nil && (h.SuccessCount+h.ErrorCount) == 0 {
		return ctx.DB().Table("job_history").Delete(h).Error
	}

	return ctx.DB().Table("job_history").Save(h).Error
}

func DeleteOldJobHistoryRows(ctx *api.Context, keepLatest int) error {
	return ctx.DB().Exec(`
    WITH ordered_history AS (
        SELECT
            id, resource_type, resource_id, name, created_at,
            ROW_NUMBER() OVER (PARTITION by resource_id, resource_type, name ORDER BY created_at DESC)
        FROM job_history
    )
    DELETE FROM job_history WHERE id IN (
        SELECT id FROM ordered_history WHERE row_number > ?
    )
    `, keepLatest).Error
}
