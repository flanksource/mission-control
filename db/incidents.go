package db

import (
	"time"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

func ReconcileIncidentStatus(ctx context.Context, incidentIDs []uuid.UUID) error {
	return ctx.DB().Exec(`
        WITH evidences_agg as (
            SELECT BOOL_AND(evidences.done) as done, incidents.status, incidents.id
            FROM evidences
            INNER JOIN hypotheses ON hypotheses.id = evidences.hypothesis_id
            INNER JOIN incidents ON incidents.id = hypotheses.incident_id
            WHERE
                incidents.status != 'closed' AND
                evidences.definition_of_done = true AND evidences.script IS NOT NULL AND evidences.script != '' AND
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

func PersistIncidentRuleFromCRD(ctx context.Context, obj *v1.IncidentRule) error {
	dbObj := api.IncidentRule{
		ID:        uuid.MustParse(string(obj.GetUID())),
		Name:      obj.Name,
		Spec:      &obj.Spec,
		Source:    models.SourceCRD,
		CreatedBy: *api.SystemUserID,
		// Gorm.Save does not use defaults when inserting
		// and the timestamp used is zero time
		CreatedAt: time.Now(),
	}

	return ctx.DB().Save(&dbObj).Error
}

func DeleteIncidentRule(ctx context.Context, id string) error {
	return ctx.DB().Table("incident_rules").Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func DeleteStaleIncidentRule(ctx context.Context, newer *v1.IncidentRule) error {
	// Incident rules have a unique index on name
	return ctx.DB().Table("incident_rules").
		Where("name = ?", newer.Name).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}
