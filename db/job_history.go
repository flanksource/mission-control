package db

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

func PersistJobHistory(ctx context.Context, h *models.JobHistory) error {
	// Delete jobs which did not process anything
	if h.ID != uuid.Nil && (h.SuccessCount+h.ErrorCount) == 0 {
		return ctx.DB().Table("job_history").Delete(h).Error
	}

	return ctx.DB().Table("job_history").Save(h).Error
}

func DeleteOldJobHistoryRows(ctx context.Context, keepLatestSuccess, keepLatestFailed int) error {
	query := `
    WITH ordered_history AS (
      SELECT
        id, 
        status,
        ROW_NUMBER() OVER (PARTITION by resource_id, name, status ORDER BY created_at DESC)
      FROM job_history
    )
    DELETE FROM job_history WHERE id IN (
      SELECT id FROM ordered_history WHERE 
        (row_number > ? AND status IN (?, ?))
        OR (row_number > ? AND status IN (?, ?))
    )`

	res := ctx.DB().Exec(query,
		keepLatestSuccess, models.StatusSuccess, models.StatusFinished,
		keepLatestFailed, models.StatusFailed, models.StatusWarning)
	if res.RowsAffected > 0 {
		logger.Infof("deleted %d job history rows", res.RowsAffected)
	}

	return res.Error
}
