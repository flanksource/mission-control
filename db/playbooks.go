package db

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func FindPlaybookRun(ctx context.Context, id uuid.UUID) (*models.PlaybookRun, error) {
	var p models.PlaybookRun
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &p, nil
}

// CanApprove returns true if the given person can approve runs of the given playbook.
func CanApprove(ctx context.Context, personID, playbookID string) (bool, error) {
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

func GetPlaybookRun(ctx context.Context, id string) (*models.PlaybookRun, error) {
	var p models.PlaybookRun
	if err := ctx.DB().Where("id = ?", id).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "playbook run(id=%s) not found", id)
		}

		return nil, dutyAPI.Errorf(dutyAPI.EINTERNAL, "something went wrong").WithDebugInfo("db.GetPlaybookRun(id=%s): %v", id, err)
	}

	return &p, nil
}

func findPlaybooksForResourceSelectable(ctx context.Context, selectable types.ResourceSelectable, selectorField string) (
	[]api.PlaybookListItem,
	[]*models.Playbook,
	error,
) {
	var playbooks []models.Playbook
	if err := ctx.DB().Model(&models.Playbook{}).Where(fmt.Sprintf("spec->>'%s' IS NOT NULL", selectorField)).Where("deleted_at IS NULL").Find(&playbooks).Error; err != nil {
		return nil, nil, fmt.Errorf("error finding playbooks with %s: %w", selectorField, err)
	}

	// To return empty list instead of null
	playbookListItems := make([]api.PlaybookListItem, 0)
	var matchedPlaybooks []*models.Playbook

	for _, pb := range playbooks {
		var spec v1.PlaybookSpec
		if err := json.Unmarshal(pb.Spec, &spec); err != nil {
			ctx.Tracef("error unmarshaling playbook[%s] spec: %v", pb.ID, err)
			continue
		}

		var resourceSelectors []types.ResourceSelector
		switch selectorField {
		case "checks":
			resourceSelectors = spec.Checks
		case "configs":
			resourceSelectors = spec.Configs
		case "components":
			resourceSelectors = spec.Components
		}

		if len(resourceSelectors) == 0 {
			continue
		}

		matches := true
		for _, rs := range resourceSelectors {
			if !rs.Matches(selectable) {
				matches = false
				break
			}
		}

		if !matches {
			continue
		}

		params, err := json.Marshal(spec.Parameters)
		if err != nil {
			return nil, nil, fmt.Errorf("error marshaling params[%v] to json: %w", spec.Parameters, err)
		}
		playbookListItems = append(playbookListItems, api.PlaybookListItem{
			ID:         pb.ID,
			Name:       pb.Name,
			Parameters: params,
		})

		matchedPlaybooks = append(matchedPlaybooks, &pb)
	}

	return playbookListItems, matchedPlaybooks, nil
}

// FindPlaybooksForCheck returns all the playbooks that match the given check type and tags.
func FindPlaybooksForCheck(ctx context.Context, check models.Check) ([]api.PlaybookListItem, []*models.Playbook, error) {
	return findPlaybooksForResourceSelectable(ctx, check, "checks")
}

// FindPlaybooksForConfig returns all the playbooks that match the given config's resource selectors
func FindPlaybooksForConfig(ctx context.Context, config models.ConfigItem) ([]api.PlaybookListItem, []*models.Playbook, error) {
	return findPlaybooksForResourceSelectable(ctx, config, "configs")
}

// FindPlaybooksForComponent returns all the playbooks that match the given component type and tags.
func FindPlaybooksForComponent(ctx context.Context, component models.Component) ([]api.PlaybookListItem, []*models.Playbook, error) {
	return findPlaybooksForResourceSelectable(ctx, component, "components")
}

func FindPlaybooksForEvent(ctx context.Context, eventClass, event string) ([]models.Playbook, error) {
	var playbooks []models.Playbook
	query := fmt.Sprintf(`SELECT * FROM playbooks WHERE spec->'on'->'%s' @> '[{"event": "%s"}]'`, eventClass, event)
	if err := ctx.DB().Raw(query).Scan(&playbooks).Error; err != nil {
		return nil, err
	}

	return playbooks, nil
}

func FindPlaybookByWebhookPath(ctx context.Context, path string) (*models.Playbook, error) {
	var p models.Playbook
	if err := ctx.DB().Debug().Where("spec->'on'->'webhook'->>'path' = ?", path).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &p, nil
}

func PersistPlaybookFromCRD(ctx context.Context, obj *v1.Playbook) error {
	_, err := SavePlaybook(ctx, obj)
	return err
}

func SavePlaybook(ctx context.Context, obj *v1.Playbook) (*models.Playbook, error) {
	playbook, err := obj.ToModel()
	if err != nil {
		return nil, err
	}

	playbook.Source = models.SourceCRD

	tx := ctx.DB().Begin()
	defer tx.Rollback()

	if playbook.ID == uuid.Nil {
		var _playbook models.Playbook
		if err := ctx.DB().Model(_playbook).Where("name = ?", playbook.Name).FirstOrInit(&_playbook).Error; err != nil {
			return nil, err
		}

		playbook.ID = _playbook.ID
	}

	if obj.Spec.On != nil && obj.Spec.On.Webhook != nil && obj.Spec.On.Webhook.Path != "" {
		existing, err := FindPlaybookByWebhookPath(ctx, obj.Spec.On.Webhook.Path)
		if err != nil {
			return nil, err
		} else if existing != nil && playbook.ID != existing.ID {
			// TODO: We can move this unique constraint handling to DB once we upgrade to Postgres 15+
			return nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "Playbook with webhook path %s already exists", obj.Spec.On.Webhook.Path)
		}
	}

	if playbook.CreatedBy == nil {
		playbook.CreatedBy = api.SystemUserID
	}

	if err := tx.Clauses(clause.Returning{}).Save(&playbook).Error; err != nil {
		return nil, err
	}

	return playbook, tx.Commit().Error
}

func DeletePlaybook(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Playbook{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// UpdatePlaybookRunStatusIfApproved updates the status of the playbook run to "pending"
// if all the approvers have approved it.
func UpdatePlaybookRunStatusIfApproved(ctx context.Context, playbookID string, approval v1.PlaybookApproval) error {
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

	tx := ctx.DB().Exec(query, approval.Approvers.Teams, approval.Approvers.People, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusPendingApproval, playbookID)
	if tx.RowsAffected > 0 {
		ctx.Tracef("[%s] %d playbook runs approved", playbookID, tx.RowsAffected)
	}
	return tx.Error
}

func SavePlaybookRunApproval(ctx context.Context, approval models.PlaybookApproval) error {
	return ctx.DB().Create(&approval).Error
}

func GetPlaybookActionsForStatus(ctx context.Context, runID uuid.UUID, statuses ...models.PlaybookActionStatus) (int64, error) {
	if len(statuses) == 0 {
		return 0, nil
	}

	var count int64
	err := ctx.DB().Model(&models.PlaybookRunAction{}).Where("playbook_run_id = ? AND status IN (?)", runID, statuses).Count(&count).Error
	return count, err
}

func GetActionStatuses(ctx context.Context, runID uuid.UUID) ([]models.PlaybookActionStatus, error) {
	var statuses []models.PlaybookActionStatus
	err := ctx.DB().Select("status").Model(&models.PlaybookRunAction{}).Where("playbook_run_id = ?", runID).Find(&statuses).Error
	return statuses, err
}
