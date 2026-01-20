package permission

import (
	"encoding/json"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

const (
	ScopeQueueSourceScope      = "scope"
	ScopeQueueSourcePermission = "permission"

	ScopeQueueActionApply   = "apply"
	ScopeQueueActionRemove  = "remove"
	ScopeQueueActionRebuild = "rebuild"
)

type ScopeMaterializationStats struct {
	Tables        map[string]int64 `json:"tables,omitempty"`
	TotalRows     int64            `json:"total_rows,omitempty"`
	SelectorCount int              `json:"selector_count,omitempty"`
	IDCount       int              `json:"id_count,omitempty"`
}

func (s *ScopeMaterializationStats) addTableCount(table string, count int64) {
	if count == 0 {
		return
	}
	if s.Tables == nil {
		s.Tables = make(map[string]int64)
	}
	s.Tables[table] += count
	s.TotalRows += count
}

type ScopeProcessor struct {
	ctx        context.Context
	sourceType string
	sourceID   uuid.UUID
	action     string
	stats      ScopeMaterializationStats
}

func GetProcessScopeJob(ctx context.Context, sourceType, sourceID, action string) (*job.Job, error) {
	if _, err := uuid.Parse(sourceID); err != nil {
		return nil, ctx.Oops().Wrapf(err, "invalid scope id %s", sourceID)
	}

	jobName := "ProcessScopeMaterialization"
	j := job.NewJob(ctx, jobName, "", func(run job.JobRuntime) error {
		processor, err := newScopeProcessor(run.Context, sourceType, sourceID, action)
		if err != nil {
			return err
		}

		run.History.AddDetails("source_type", sourceType)
		run.History.AddDetails("source_id", sourceID)
		run.History.AddDetails("action", action)

		if err := processor.Run(); err != nil {
			return err
		}

		run.History.AddDetails("tables", processor.stats.Tables)
		run.History.AddDetails("total_rows", processor.stats.TotalRows)
		run.History.AddDetails("selector_count", processor.stats.SelectorCount)
		run.History.AddDetails("id_count", processor.stats.IDCount)

		if processor.stats.TotalRows > 0 {
			run.History.SuccessCount = int(processor.stats.TotalRows)
		}

		return nil
	})

	j.ResourceType = sourceType
	j.ResourceID = sourceID
	j.ID = action
	j.JobHistory = true
	return j, nil
}

func newScopeProcessor(ctx context.Context, sourceType, sourceID, action string) (*ScopeProcessor, error) {
	id, err := uuid.Parse(sourceID)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "invalid scope id %s", sourceID)
	}

	return &ScopeProcessor{
		ctx:        ctx,
		sourceType: sourceType,
		sourceID:   id,
		action:     action,
		stats:      ScopeMaterializationStats{},
	}, nil
}

func (p *ScopeProcessor) Run() error {
	switch p.sourceType {
	case ScopeQueueSourceScope:
		return p.runScope()
	case ScopeQueueSourcePermission:
		return p.runPermission()
	default:
		return p.ctx.Oops().Errorf("unknown scope source_type=%s", p.sourceType)
	}
}

func (p *ScopeProcessor) runScope() error {
	switch p.action {
	case ScopeQueueActionRemove:
		removed, err := removeScopeFromAllTables(p.ctx, p.sourceID.String())
		if err != nil {
			return err
		}
		for table, count := range removed {
			p.stats.addTableCount(table, count)
		}
		return nil
	case ScopeQueueActionApply:
		return applyScope(p.ctx, p.sourceID, &p.stats)
	case ScopeQueueActionRebuild:
		removed, err := removeScopeFromAllTables(p.ctx, p.sourceID.String())
		if err != nil {
			return err
		}
		for table, count := range removed {
			p.stats.addTableCount(table, count)
		}
		return applyScope(p.ctx, p.sourceID, &p.stats)
	default:
		return p.ctx.Oops().Errorf("unknown scope action=%s", p.action)
	}
}

func (p *ScopeProcessor) runPermission() error {
	switch p.action {
	case ScopeQueueActionRemove:
		removed, err := removeScopeFromAllTables(p.ctx, p.sourceID.String())
		if err != nil {
			return err
		}
		for table, count := range removed {
			p.stats.addTableCount(table, count)
		}
		return nil
	case ScopeQueueActionApply:
		return applyPermissionScope(p.ctx, p.sourceID, &p.stats)
	case ScopeQueueActionRebuild:
		removed, err := removeScopeFromAllTables(p.ctx, p.sourceID.String())
		if err != nil {
			return err
		}
		for table, count := range removed {
			p.stats.addTableCount(table, count)
		}
		return applyPermissionScope(p.ctx, p.sourceID, &p.stats)
	default:
		return p.ctx.Oops().Errorf("unknown permission action=%s", p.action)
	}
}

func applyScope(ctx context.Context, scopeID uuid.UUID, stats *ScopeMaterializationStats) error {
	var scope models.Scope
	err := ctx.DB().Where("id = ? AND deleted_at IS NULL", scopeID).First(&scope).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return ctx.Oops().Wrap(err)
	}

	var targets []v1.ScopeTarget
	if err := json.Unmarshal([]byte(scope.Targets), &targets); err != nil {
		return ctx.Oops().Wrapf(err, "failed to unmarshal scope targets")
	}

	for _, target := range targets {
		switch {
		case target.Config != nil:
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "config_items", convertScopeResourceSelector(target.Config), scopeID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("config_items", count)
			}
		case target.Component != nil:
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "components", convertScopeResourceSelector(target.Component), scopeID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("components", count)
			}
		case target.Canary != nil:
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "canaries", convertScopeResourceSelector(target.Canary), scopeID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("canaries", count)
			}
		case target.Playbook != nil:
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "playbooks", convertScopeResourceSelector(target.Playbook), scopeID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("playbooks", count)
			}
		case target.View != nil:
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "views", convertScopeResourceSelector(target.View), scopeID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("views", count)
			}
		case target.Global != nil:
			globalSelector := convertScopeResourceSelector(target.Global)
			for _, table := range rlsScopeTables() {
				if stats != nil {
					stats.SelectorCount++
				}
				count, err := applyScopeSelector(ctx, table, globalSelector, scopeID.String())
				if err != nil {
					return err
				}
				if stats != nil {
					stats.addTableCount(table, count)
				}
			}
		}
	}

	return nil
}

func applyPermissionScope(ctx context.Context, permissionID uuid.UUID, stats *ScopeMaterializationStats) error {
	var permission models.Permission
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", permissionID).First(&permission).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return ctx.Oops().Wrap(err)
	}

	if !collections.MatchItems(policy.ActionRead, strings.Split(permission.Action, ",")...) {
		return nil
	}

	if permission.ConfigID != nil {
		if stats != nil {
			stats.IDCount++
		}
		count, err := applyScopeToIDs(ctx, "config_items", []uuid.UUID{*permission.ConfigID}, permission.ID.String())
		if err != nil {
			return err
		}
		if stats != nil {
			stats.addTableCount("config_items", count)
		}
	}
	if permission.ComponentID != nil {
		if stats != nil {
			stats.IDCount++
		}
		count, err := applyScopeToIDs(ctx, "components", []uuid.UUID{*permission.ComponentID}, permission.ID.String())
		if err != nil {
			return err
		}
		if stats != nil {
			stats.addTableCount("components", count)
		}
	}
	if permission.CanaryID != nil {
		if stats != nil {
			stats.IDCount++
		}
		count, err := applyScopeToIDs(ctx, "canaries", []uuid.UUID{*permission.CanaryID}, permission.ID.String())
		if err != nil {
			return err
		}
		if stats != nil {
			stats.addTableCount("canaries", count)
		}
	}
	if permission.PlaybookID != nil {
		if stats != nil {
			stats.IDCount++
		}
		count, err := applyScopeToIDs(ctx, "playbooks", []uuid.UUID{*permission.PlaybookID}, permission.ID.String())
		if err != nil {
			return err
		}
		if stats != nil {
			stats.addTableCount("playbooks", count)
		}
	}

	if len(permission.ObjectSelector) == 0 {
		return nil
	}

	var selectors v1.PermissionObject
	if err := json.Unmarshal([]byte(permission.ObjectSelector), &selectors); err != nil {
		return ctx.Oops().Wrapf(err, "failed to unmarshal permission object_selector")
	}

	if len(selectors.Configs) > 0 {
		for _, selector := range selectors.Configs {
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "config_items", selector, permission.ID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("config_items", count)
			}
		}
	}
	if len(selectors.Components) > 0 {
		for _, selector := range selectors.Components {
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "components", selector, permission.ID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("components", count)
			}
		}
	}
	if len(selectors.Playbooks) > 0 {
		for _, selector := range selectors.Playbooks {
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "playbooks", selector, permission.ID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("playbooks", count)
			}
		}
	}
	if len(selectors.Views) > 0 {
		for _, selector := range selectors.Views {
			viewSelector := types.ResourceSelector{
				Name:      selector.Name,
				Namespace: selector.Namespace,
			}
			if stats != nil {
				stats.SelectorCount++
			}
			count, err := applyScopeSelector(ctx, "views", viewSelector, permission.ID.String())
			if err != nil {
				return err
			}
			if stats != nil {
				stats.addTableCount("views", count)
			}
		}
	}

	return nil
}

func applyScopeSelector(ctx context.Context, table string, selector types.ResourceSelector, scopeID string) (int64, error) {
	if selector.IsEmpty() {
		return 0, nil
	}

	batchSize := ctx.Properties().Int("rls.scope.materialize.batch", 10000)
	var total int64
	for {
		q := ctx.DB().Table(table).Select("id")
		q, err := query.SetResourceSelectorClause(ctx, selector, q, table)
		if err != nil {
			return 0, ctx.Oops().Wrapf(err, "failed to apply resource selector to %s", table)
		}

		q = q.Where("NOT (COALESCE(__scope, '{}'::uuid[]) @> ARRAY[?]::uuid[])", scopeID)
		if batchSize > 0 {
			q = q.Limit(batchSize)
		}

		var ids []uuid.UUID
		if err := q.Find(&ids).Error; err != nil {
			return 0, ctx.Oops().Wrapf(err, "failed to fetch scope ids for %s", table)
		}

		if len(ids) == 0 {
			break
		}

		if err := ctx.DB().Table(table).
			Where("id IN ?", ids).
			UpdateColumn("__scope", gorm.Expr("array_append(COALESCE(__scope, '{}'::uuid[]), ?)", scopeID)).Error; err != nil {
			return 0, ctx.Oops().Wrapf(err, "failed to update __scope for %s", table)
		}
		total += int64(len(ids))
	}

	return total, nil
}

func applyScopeToIDs(ctx context.Context, table string, ids []uuid.UUID, scopeID string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	result := ctx.DB().Table(table).
		Where("id IN ?", ids).
		Where("NOT (COALESCE(__scope, '{}'::uuid[]) @> ARRAY[?]::uuid[])", scopeID).
		UpdateColumn("__scope", gorm.Expr("array_append(COALESCE(__scope, '{}'::uuid[]), ?)", scopeID))
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func removeScopeFromAllTables(ctx context.Context, scopeID string) (map[string]int64, error) {
	removed := make(map[string]int64)
	for _, table := range rlsScopeTables() {
		count, err := removeScopeFromTable(ctx, table, scopeID)
		if err != nil {
			return nil, err
		}
		removed[table] = count
	}

	return removed, nil
}

func removeScopeFromTable(ctx context.Context, table, scopeID string) (int64, error) {
	result := ctx.DB().Table(table).
		Where("__scope @> ARRAY[?]::uuid[]", scopeID).
		UpdateColumn("__scope", gorm.Expr("array_remove(__scope, ?)", scopeID))
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func rlsScopeTables() []string {
	return []string{"config_items", "components", "canaries", "playbooks", "views"}
}

func convertScopeResourceSelector(selector *v1.ScopeResourceSelector) types.ResourceSelector {
	return types.ResourceSelector{
		Agent:       selector.Agent,
		Name:        selector.Name,
		Namespace:   selector.Namespace,
		TagSelector: selector.TagSelector,
	}
}
