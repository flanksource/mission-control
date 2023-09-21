package db

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func FindPlaybooksForEvent(ctx api.Context, eventClass, event string) ([]models.Playbook, error) {
	var playbooks []models.Playbook
	query := fmt.Sprintf(`SELECT * FROM playbooks WHERE spec->'on'->'%s' @> '[{"event": "%s"}]'`, eventClass, event)
	if err := ctx.DB().Raw(query).Scan(&playbooks).Error; err != nil {
		return nil, err
	}

	return playbooks, nil
}

func FindPlaybook(ctx api.Context, id uuid.UUID) (*models.Playbook, error) {
	var p models.Playbook
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &p, nil
}

// CanApprove returns true if the given person can approve runs of the given playbook.
func CanApprove(ctx api.Context, personID, playbookID string) (bool, error) {
	query := `
	WITH playbook_approvers AS (
		SELECT id,
			ARRAY(SELECT jsonb_array_elements_text(spec->'approval'->'approvers'->'teams')) teams,
			ARRAY(SELECT jsonb_array_elements_text(spec->'approval'->'approvers'->'people')) people
		FROM playbooks
		WHERE id = ?
	)
	SELECT COUNT(*) FROM playbook_approvers WHERE 
	CAST(playbook_approvers.teams AS text[]) && ( -- check if the person belongs to a team that can approve
		SELECT array_agg(teams.name) FROM teams LEFT JOIN team_members 
		ON teams.id = team_members.team_id 
		WHERE person_id = ?
	)
	OR
	CAST(playbook_approvers.people AS text[]) @> ARRAY( -- check if the person is an approver
		SELECT email FROM people WHERE id = ?
	)`

	var count int
	if err := ctx.DB().Raw(query, playbookID, personID, personID).Scan(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func GetPlaybookRun(ctx api.Context, id string) (*models.PlaybookRun, error) {
	var p models.PlaybookRun
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, api.Errorf(api.ENOTFOUND, "playbook run(id=%s) not found", id)
		}

		return nil, api.Errorf(api.EINTERNAL, "something went wrong").WithDebugInfo("db.GetPlaybookRun(id=%s): %v", id, err)
	}

	return &p, nil
}

// FindPlaybooksForCheck returns all the playbooks that match the given check type and tags.
func FindPlaybooksForCheck(ctx api.Context, configType string, tags map[string]string) ([]models.Playbook, error) {
	joinQuery := `JOIN LATERAL jsonb_array_elements(playbooks."spec"->'checks') AS checks(ch) ON 1=1`
	var joinArgs []any
	if len(tags) != 0 {
		joinQuery += " AND (?::jsonb) @> (checks.ch->'tags')"
		joinArgs = append(joinArgs, types.JSONStringMap(tags))
	}
	if configType != "" {
		joinQuery += " AND checks.ch->>'type' = ?"
		joinArgs = append(joinArgs, configType)
	}

	query := ctx.DB().Debug().
		Select("DISTINCT playbooks.*").
		Joins(joinQuery, joinArgs...)

	var playbooks []models.Playbook
	err := query.Find(&playbooks).Error
	return playbooks, err
}

// FindPlaybooksForConfig returns all the playbooks that match the given config type and tags.
func FindPlaybooksForConfig(ctx api.Context, configType string, tags map[string]string) ([]models.Playbook, error) {
	joinQuery := `JOIN LATERAL jsonb_array_elements(playbooks."spec"->'configs') AS configs(config) ON 1=1`
	var joinArgs []any

	if len(tags) != 0 {
		joinQuery += " AND (?::jsonb) @> (configs.config->'tags')"
		joinArgs = append(joinArgs, types.JSONStringMap(tags))
	}
	if configType != "" {
		joinQuery += " AND configs.config->>'type' = ?"
		joinArgs = append(joinArgs, configType)
	}

	query := ctx.DB().
		Select("DISTINCT playbooks.*").
		Joins(joinQuery, joinArgs...)

	var playbooks []models.Playbook
	err := query.Find(&playbooks).Error
	return playbooks, err
}

// FindPlaybooksForComponent returns all the playbooks that match the given component type and tags.
func FindPlaybooksForComponent(ctx api.Context, configType string, tags map[string]string) ([]models.Playbook, error) {
	joinQuery := `JOIN LATERAL jsonb_array_elements(playbooks."spec"->'components') AS components(component) ON 1=1`
	var joinArgs []any

	if len(tags) != 0 {
		joinQuery += " AND (?::jsonb) @> (components.component->'tags')"
		joinArgs = append(joinArgs, types.JSONStringMap(tags))
	}
	if configType != "" {
		joinQuery += " AND components.component->>'type' = ?"
		joinArgs = append(joinArgs, configType)
	}

	query := ctx.DB().
		Select("DISTINCT playbooks.*").
		Joins(joinQuery, joinArgs...)

	var playbooks []models.Playbook
	err := query.Find(&playbooks).Error
	return playbooks, err
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

// UpdatePlaybookRunStatusIfApproved updates the status of the playbook run to "pending"
// if all the approvers have approved it.
func UpdatePlaybookRunStatusIfApproved(ctx api.Context, playbookID string, approval v1.PlaybookApproval) error {
	if approval.Approvers.Empty() {
		return nil
	}

	operator := `@>`
	if approval.Type == v1.PlaybookApprovalTypeAny {
		operator = `&&`
	}

	query := fmt.Sprintf(`
	WITH run_approvals AS	(
		SELECT run_id, ARRAY_AGG(COALESCE(person_id, team_id)) AS approvers
		FROM playbook_approvals
		GROUP BY run_id
	),
	allowed_approvers AS (
		SELECT id FROM teams WHERE name IN ?
		UNION
		SELECT id FROM people WHERE email IN ?
	)
	UPDATE playbook_runs SET status = ? WHERE
	status = ?
	AND playbook_id = ?
	AND id IN (
		SELECT run_id FROM run_approvals WHERE approvers %s (SELECT array_agg(id) FROM allowed_approvers)
	)`, operator)

	return ctx.DB().Exec(query, approval.Approvers.Teams, approval.Approvers.People, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusPending, playbookID).Error
}

func SavePlaybookRunApproval(ctx api.Context, approval models.PlaybookApproval) error {
	return ctx.DB().Create(&approval).Error
}
