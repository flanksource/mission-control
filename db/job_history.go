package db

import (
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

func PersistJobHistory(h *models.JobHistory) error {
	if Gorm == nil {
		return nil
	}

	// Delete jobs which did not process anything
	if h.ID != uuid.Nil && (h.SuccessCount+h.ErrorCount) == 0 {
		return Gorm.Table("job_history").Delete(h).Error
	}

	return Gorm.Table("job_history").Save(h).Error
}

func DeleteOldJobHistoryRows(keepLatest int) error {
	return Gorm.Exec(`
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
