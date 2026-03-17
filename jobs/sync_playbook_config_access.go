package jobs

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const configAccessSourceRBAC = "missioncontrol::rbac"

type configAccessSyncResult struct {
	Inserted int64 `json:"inserted"`
	Deleted  int64 `json:"deleted"`
}

func SyncPlaybookConfigAccess(ctx context.Context) *job.Job {
	return &job.Job{
		Name:       "SyncPlaybookConfigAccess",
		Context:    ctx,
		Schedule:   "@every 4h",
		Singleton:  true,
		JobHistory: true,
		Retention:  job.RetentionFew,
		RunNow:     true,
		Fn: func(jr job.JobRuntime) error {
			result, err := syncPlaybookConfigAccess(jr.Context)
			jr.History.AddDetails("inserted", result.Inserted)
			jr.History.AddDetails("deleted", result.Deleted)
			jr.History.SuccessCount = int(result.Inserted + result.Deleted)
			return err
		},
	}
}

// syncPlaybookConfigAccess populates the config_access table by checking every
// person × playbook combination against the casbin enforcer for playbook:run
// and playbook:approve permissions.
//
// This is a brute-force approach: for 50 users and 200 playbooks, it makes
// 50 × 200 × 2 = 20,000 HasPermission calls. This is viable because:
//   - mission-control is not designed for hundreds of users or thousands of playbooks
//   - the casbin enforcer caches evaluation results
//   - this runs as a background job, not in a request path
//
// On a steady-state run with no permission changes, this results in zero writes.
func syncPlaybookConfigAccess(ctx context.Context) (configAccessSyncResult, error) {
	var result configAccessSyncResult

	// 1. Get active playbook config item IDs.
	var playbookIDs []uuid.UUID
	if err := ctx.DB().
		Model(&models.ConfigItem{}).
		Select("id").
		Where("type = ?", "MissionControl::Playbook").
		Where("deleted_at IS NULL").
		Find(&playbookIDs).Error; err != nil {
		return result, fmt.Errorf("failed to query playbook config items: %w", err)
	}

	// 2. Get active people with their mapped active external user IDs.
	// System scraper stores people linkage in external_users.aliases (people:<person-id>).
	type personExternalUser struct {
		PersonID       uuid.UUID `gorm:"column:person_id"`
		ExternalUserID uuid.UUID `gorm:"column:external_user_id"`
	}

	var people []personExternalUser
	if err := ctx.DB().Raw(`
		SELECT p.id AS person_id, eu.id AS external_user_id
		FROM people p
		INNER JOIN external_users eu
			ON ('people:' || p.id::text) = ANY(eu.aliases)
		WHERE p.deleted_at IS NULL
		  AND eu.deleted_at IS NULL
	`).Scan(&people).Error; err != nil {
		return result, fmt.Errorf("failed to query active people mapped to active external users: %w", err)
	}

	// 3. Resolve external role IDs for playbook actions.
	type playbookRoleIDs struct {
		RunRoleID     *uuid.UUID `gorm:"column:run_role_id"`
		ApproveRoleID *uuid.UUID `gorm:"column:approve_role_id"`
	}

	var roleIDs playbookRoleIDs
	if err := ctx.DB().Raw(`
		SELECT
			(SELECT id FROM external_roles WHERE deleted_at IS NULL AND ? = ANY(aliases) LIMIT 1) AS run_role_id,
			(SELECT id FROM external_roles WHERE deleted_at IS NULL AND ? = ANY(aliases) LIMIT 1) AS approve_role_id
	`, "role:playbook:run", "role:playbook:approve").Scan(&roleIDs).Error; err != nil {
		return result, fmt.Errorf("failed to query external role ids: %w", err)
	}

	hasRunRole := roleIDs.RunRoleID != nil
	hasApproveRole := roleIDs.ApproveRoleID != nil

	runRoleID := uuid.Nil
	if hasRunRole {
		runRoleID = *roleIDs.RunRoleID
	}

	approveRoleID := uuid.Nil
	if hasApproveRole {
		approveRoleID = *roleIDs.ApproveRoleID
	}

	// 4. For each person × playbook, check permissions via the casbin enforcer.
	// Collect desired tuples, deduplicating via map.
	type accessKey struct {
		ConfigID       uuid.UUID
		ExternalUserID uuid.UUID
		ExternalRoleID uuid.UUID
	}

	desired := make(map[accessKey]struct{})

	for _, person := range people {
		for _, playbookID := range playbookIDs {
			attr := &models.ABACAttribute{Playbook: models.Playbook{ID: playbookID}}

			if hasRunRole && rbac.HasPermission(ctx, person.PersonID.String(), attr, policy.ActionPlaybookRun) {
				desired[accessKey{ConfigID: playbookID, ExternalUserID: person.ExternalUserID, ExternalRoleID: runRoleID}] = struct{}{}
			}

			if hasApproveRole && rbac.HasPermission(ctx, person.PersonID.String(), attr, policy.ActionPlaybookApprove) {
				desired[accessKey{ConfigID: playbookID, ExternalUserID: person.ExternalUserID, ExternalRoleID: approveRoleID}] = struct{}{}
			}
		}
	}

	// 5. Bulk-load desired tuples into a temp table via COPY, then use SQL to diff.
	pool := ctx.Pool()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE _desired_config_access (
			config_id        UUID NOT NULL,
			external_user_id UUID NOT NULL,
			external_role_id UUID NOT NULL
		) ON COMMIT DROP
	`); err != nil {
		return result, fmt.Errorf("failed to create temp table: %w", err)
	}

	rows := make([][]any, 0, len(desired))
	for key := range desired {
		rows = append(rows, []any{key.ConfigID, key.ExternalUserID, key.ExternalRoleID})
	}

	if _, err := tx.CopyFrom(ctx,
		pgx.Identifier{"_desired_config_access"},
		[]string{"config_id", "external_user_id", "external_role_id"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return result, fmt.Errorf("failed to COPY into temp table: %w", err)
	}

	if _, err := tx.Exec(ctx, "ANALYZE _desired_config_access"); err != nil {
		return result, fmt.Errorf("failed to ANALYZE temp table: %w", err)
	}

	// 6. Insert entries present in desired but missing from config_access
	insertTag, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO config_access (config_id, external_user_id, external_role_id, source, created_at)
		SELECT d.config_id, d.external_user_id, d.external_role_id, '%s', now()
		FROM _desired_config_access d
		WHERE NOT EXISTS (
			SELECT 1 FROM config_access ca
			WHERE ca.config_id IS NOT DISTINCT FROM d.config_id
			  AND ca.external_user_id IS NOT DISTINCT FROM d.external_user_id
			  AND ca.external_role_id IS NOT DISTINCT FROM d.external_role_id
			  AND ca.source = '%s'
			  AND ca.deleted_at IS NULL
		)
	`, configAccessSourceRBAC, configAccessSourceRBAC))
	if err != nil {
		return result, fmt.Errorf("failed to insert new config access: %w", err)
	}

	// 7. Soft-delete entries in config_access that are no longer desired
	deleteTag, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE config_access SET deleted_at = now()
		WHERE source = '%s'
		  AND deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM _desired_config_access d
			WHERE d.config_id IS NOT DISTINCT FROM config_access.config_id
			  AND d.external_user_id IS NOT DISTINCT FROM config_access.external_user_id
			  AND d.external_role_id IS NOT DISTINCT FROM config_access.external_role_id
		  )
	`, configAccessSourceRBAC))
	if err != nil {
		return result, fmt.Errorf("failed to soft-delete stale config access: %w", err)
	}

	result.Inserted = insertTag.RowsAffected()
	result.Deleted = deleteTag.RowsAffected()

	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("failed to commit transaction: %w", err)
	}

	if result.Inserted > 0 {
		ctx.Infof("config_access: inserted %d new RBAC entries", result.Inserted)
	}
	if result.Deleted > 0 {
		ctx.Infof("config_access: soft-deleted %d stale RBAC entries", result.Deleted)
	}

	return result, nil
}
