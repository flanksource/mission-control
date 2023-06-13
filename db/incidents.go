package db

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

func ReconcileIncidentStatus(incidentIDs []uuid.UUID) error {
	return Gorm.Exec(`
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
        WHERE incidents.id = evidences_agg.id
    `, incidentIDs).Error
}

func PersistIncidentRuleFromCRD(obj *v1.IncidentRule) error {
	dbObj := api.IncidentRule{
		ID:     uuid.MustParse(string(obj.GetUID())),
		Name:   obj.Name,
		Spec:   &obj.Spec,
		Source: models.SourceCRD,
	}

	return Gorm.Save(&dbObj).Error
}

func DeleteIncidentRule(id string) error {
	return Gorm.Table("incident_rules").
		Delete(&api.IncidentRule{}, "id = ?", id).
		Error
}
