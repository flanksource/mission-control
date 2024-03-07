package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/eko/gocache/lib/v4/cache"
	gocache_store "github.com/eko/gocache/store/go_cache/v4"
	gocache "github.com/patrickmn/go-cache"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func FindPlaybooksForEvent(ctx context.Context, eventClass, event string) ([]models.Playbook, error) {
	var playbooks []models.Playbook
	query := fmt.Sprintf(`SELECT * FROM playbooks WHERE spec->'on'->'%s' @> '[{"event": "%s"}]'`, eventClass, event)
	if err := ctx.DB().Raw(query).Scan(&playbooks).Error; err != nil {
		return nil, err
	}

	return playbooks, nil
}

func FindPlaybook(ctx context.Context, id uuid.UUID) (*models.Playbook, error) {
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

// FindPlaybooksForCheck returns all the playbooks that match the given check type and tags.
func FindPlaybooksForCheck(ctx context.Context, checkType string, tags map[string]string) ([]api.PlaybookListItem, error) {
	joinQuery := `JOIN LATERAL jsonb_array_elements(playbooks."spec"->'checks') AS checks(ch) ON 1=1`
	var joinArgs []any
	if len(tags) != 0 {
		joinQuery += " AND (?::jsonb) @> COALESCE(checks.ch->'tags', '{}'::jsonb)"
		joinArgs = append(joinArgs, types.JSONStringMap(tags))
	}
	if checkType != "" && checkType != "*" {
		joinQuery += " AND checks.ch->>'type' = ?"
		joinArgs = append(joinArgs, checkType)
	}

	query := ctx.DB().
		Select("DISTINCT ON (playbooks.id) playbooks.id, playbooks.name, playbooks.spec->'parameters' as parameters").
		Joins(joinQuery, joinArgs...)

	var playbooks []api.PlaybookListItem
	err := query.Model(&models.Playbook{}).Find(&playbooks).Error
	return playbooks, err
}

// <id> -> models.Component
var configPlaybookCache = cache.New[[]api.PlaybookListItem](gocache_store.NewGoCache(gocache.New(30*time.Minute, 30*time.Minute)))

// FindPlaybooksForConfig returns all the playbooks that match the given config's resource selectors
func FindPlaybooksForConfig(ctx context.Context, configID string) ([]api.PlaybookListItem, error) {
	logger.Infof("YASH 1")
	if val, err := configPlaybookCache.Get(ctx, configID); err == nil {
		return val, nil
	}

	var playbooks []models.Playbook
	logger.Infof("YASH 2")
	if err := ctx.DB().Model(&models.Playbook{}).Where("spec->>'configs' IS NOT NULL").Where("deleted_at IS NULL").Find(&playbooks).Error; err != nil {
		return nil, fmt.Errorf("error finding playbooks with configs: %w", err)
	}

	configIDPlaybooks := make(map[string][]string)
	logger.Infof("YASH 3")
	for _, pb := range playbooks {
		var spec v1.PlaybookSpec
		logger.Infof("YASH 3.5 SPEC %s", pb.Spec.String())
		if err := json.Unmarshal(pb.Spec, &spec); err != nil {
			return nil, fmt.Errorf("error unmarshaling playbook spec: %w", err)
		}

		logger.Infof("YASH 4 SPEC CONFIGS %s", spec.Configs)
		configIDs, err := query.FindConfigIDsByResourceSelector(ctx, spec.Configs...)
		if err != nil {
			return nil, fmt.Errorf("error finding config ids by resource selector: %w", err)
		}

		logger.Infof("YASH 5 SPEC config_ids %v", configIDs)
		for _, cid := range configIDs {
			configIDPlaybooks[cid.String()] = append(configIDPlaybooks[cid.String()], pb.ID.String())
		}
	}

	var playbookListItems []api.PlaybookListItem
	err := ctx.DB().
		Model(&models.Playbook{}).
		Select("id", "name", "playbooks.spec->'parameters' as parameters").
		Where("id in ?", configIDPlaybooks[configID]).
		Find(&playbookListItems).Error
	if err != nil {
		return nil, fmt.Errorf("error querying playbooks: %w", err)
	}

	if err := configPlaybookCache.Set(ctx, configID, playbookListItems); err != nil {
		return nil, fmt.Errorf("error caching playbooks for config: %w", err)
	}

	return playbookListItems, err
}

// FindPlaybooksForComponent returns all the playbooks that match the given component type and tags.
func FindPlaybooksForComponent(ctx context.Context, componentType string, tags map[string]string) ([]api.PlaybookListItem, error) {
	joinQuery := `JOIN LATERAL jsonb_array_elements(playbooks."spec"->'components') AS components(component) ON 1=1`
	var joinArgs []any

	if len(tags) != 0 {
		joinQuery += " AND (?::jsonb) @> COALESCE(components.component->'tags', '{}'::jsonb)"
		joinArgs = append(joinArgs, types.JSONStringMap(tags))
	}
	if componentType != "" && componentType != "*" {
		joinQuery += " AND components.component->>'type' = ?"
		joinArgs = append(joinArgs, componentType)
	}

	query := ctx.DB().
		Select("DISTINCT ON (playbooks.id) playbooks.id, playbooks.name, playbooks.spec->'parameters' as parameters").
		Joins(joinQuery, joinArgs...)

	var playbooks []api.PlaybookListItem
	err := query.Model(&models.Playbook{}).Find(&playbooks).Error
	return playbooks, err
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
	specJSON, err := json.Marshal(obj.Spec)
	if err != nil {
		return err
	}

	tx := ctx.DB().Begin()
	defer tx.Rollback()

	if obj.Spec.On != nil && obj.Spec.On.Webhook != nil && obj.Spec.On.Webhook.Path != "" {
		playbook, err := FindPlaybookByWebhookPath(ctx, obj.Spec.On.Webhook.Path)
		if err != nil {
			return err
		} else if playbook != nil {
			// TODO: We can move this unique constraint handling to DB once we upgrade to Postgres 15+
			if playbook.ID.String() != string(obj.GetUID()) {
				return dutyAPI.Errorf(dutyAPI.ECONFLICT, "Playbook with webhook path %s already exists", obj.Spec.On.Webhook.Path)
			}
		}
	}

	dbObj := models.Playbook{
		ID:        uuid.MustParse(string(obj.GetUID())),
		Name:      obj.Name,
		Spec:      specJSON,
		Source:    models.SourceCRD,
		CreatedBy: api.SystemUserID,
	}

	if err := tx.Save(&dbObj).Error; err != nil {
		return err
	}

	return tx.Commit().Error
}

func DeletePlaybook(ctx context.Context, id string) error {
	return ctx.DB().Delete(&models.Playbook{}, "id = ?", id).Error
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

	tx := ctx.DB().Exec(query, approval.Approvers.Teams, approval.Approvers.People, models.PlaybookRunStatusScheduled, models.PlaybookRunStatusPending, playbookID)
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
