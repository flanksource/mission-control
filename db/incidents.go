package db

import (
	"github.com/flanksource/commons/logger"
	"github.com/google/uuid"
)

func ReconcileIncidentStatus(incidentIDs []uuid.UUID) {
	tx := Gorm.Exec(`
        WITH evidences_agg as (
            SELECT BOOL_AND(evidences.done) as done, incidents.status, incidents.id
            FROM evidences
            INNER JOIN hypotheses ON hypotheses.id = evidences.hypothesis_id
            INNER JOIN incidents ON incidents.id = hypotheses.incident_id
            WHERE
                incidents.status != 'closed' AND
                evidences.definition_of_done = true AND
                incidents.id IN (?)
            GROUP BY incidents.id
        )
        UPDATE incidents
        SET status = (
            CASE
                WHEN evidences_agg.done = true AND evidences_agg.status != 'resolved' THEN 'resolved'
                WHEN evidences_agg.done = false AND evidences_agg.status = 'resolved' THEN 'open'
                ELSE evidences_agg.status
            END
        )
        FROM evidences_agg
    `, incidentIDs)

	if tx.Error != nil {
		logger.Errorf("Error updating incident status: %v", tx.Error)
	}
}
