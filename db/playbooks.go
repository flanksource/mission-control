package db

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

func FindPlaybook(ctx *api.Context, id uuid.UUID) (*models.Playbook, error) {
	var p models.Playbook
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &p, nil
}

func FindPlaybookRun(ctx *api.Context, id string) (*models.PlaybookRun, error) {
	var p models.PlaybookRun
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &p, nil
}

// FindPlaybooksByTypeAndTags returns all the playbooks that match the given type and tags.
func FindPlaybooksByTypeAndTags(ctx *api.Context, configType string, tags map[string]string) ([]models.Playbook, error) {
	joinQuery := `JOIN LATERAL jsonb_array_elements(playbooks."spec"->'configs') AS configs(config) ON 1=1`
	if tags != nil {
		joinQuery += " AND (?::jsonb) @> (configs.config->'tags')"
	}
	if configType != "" {
		joinQuery += " AND configs.config->>'type' = ?"
	}

	query := ctx.DB().
		Select("DISTINCT playbooks.*").
		Joins(joinQuery, types.JSONStringMap(tags), configType)

	var playbooks []models.Playbook
	err := query.Find(&playbooks).Error
	return playbooks, err
}

// GetScheduledPlaybookRuns returns all the scheduled playbook runs that should be started
// before X duration from now.
func GetScheduledPlaybookRuns(ctx *api.Context, startingBefore time.Duration, exceptions ...uuid.UUID) ([]models.PlaybookRun, error) {
	var runs []models.PlaybookRun
	if err := ctx.DB().Not(exceptions).Where("start_time <= NOW() + ?", startingBefore).Where("status = ?", models.PlaybookRunStatusScheduled).Order("start_time").Find(&runs).Error; err != nil {
		return nil, err
	}

	return runs, nil
}

func PersistPlaybookFromCRD(obj *v1.Playbook) error {
	specJSON, err := json.Marshal(obj.Spec)
	if err != nil {
		return err
	}

	dbObj := models.Playbook{
		ID:        uuid.MustParse(string(obj.GetUID())),
		Name:      obj.Name,
		Spec:      specJSON,
		Source:    models.SourceCRD,
		CreatedBy: api.SystemUserID,
	}

	return Gorm.Save(&dbObj).Error
}

func DeletePlaybook(id string) error {
	return Gorm.Delete(&models.Playbook{}, "id = ?", id).Error
}

func UpdateApprovedPlaybookRuns(ctx *api.Context, playbookID string, approverIDs []string) error {
	query := `
	WITH run_approvals AS	(
		SELECT run_id, ARRAY_AGG(COALESCE(person_id, team_id)) AS ids
		FROM playbook_approvals
		GROUP BY run_id
	)
	UPDATE playbook_runs SET status = ? WHERE
	status = ?
	AND playbook_id = ?
	AND id IN (SELECT run_id FROM run_approvals WHERE ids @> ?)`

	return ctx.DB().Debug().Exec(query, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusPending, playbookID, pq.Array(approverIDs)).Error
}
