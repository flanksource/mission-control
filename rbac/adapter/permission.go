package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	pkgPolicy "github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

type PermissionAdapter struct {
	*gormadapter.Adapter // gorm adapter for `casbin_rules` table

	db *gorm.DB
}

var _ persist.BatchAdapter = &PermissionAdapter{}

func NewPermissionAdapter(db *gorm.DB, main *gormadapter.Adapter) persist.Adapter {
	return &PermissionAdapter{
		db:      db,
		Adapter: main,
	}
}

func (a *PermissionAdapter) LoadPolicy(model model.Model) error {
	if err := a.Adapter.LoadPolicy(model); err != nil {
		return err
	}

	var permissions []models.Permission
	if err := a.db.Where("deleted_at IS NULL").Find(&permissions).Error; err != nil {
		return fmt.Errorf("failed to load permissions: %w", err)
	}

	for _, permission := range permissions {
		// Expand scope references in object_selector before converting to Casbin rules
		expandedPerm, err := a.expandPermissionScopes(permission)
		if err != nil {
			return err
		}

		policies := PermissionToCasbinRule(expandedPerm)
		for _, policy := range policies {
			if err := persist.LoadPolicyArray(policy, model); err != nil {
				return err
			}
		}
	}

	var permissionGroups []models.PermissionGroup
	if err := a.db.Where("deleted_at IS NULL").Find(&permissionGroups).Error; err != nil {
		return fmt.Errorf("failed to load permissions: %w", err)
	}

	for _, pg := range permissionGroups {
		policies, err := a.permissionGroupToCasbinRule(pg)
		if err != nil {
			return err
		}

		for _, policy := range policies {
			if err := persist.LoadPolicyArray(policy, model); err != nil {
				return err
			}
		}
	}

	return nil
}

func PermissionToCasbinRule(permission models.Permission) [][]string {
	var policies [][]string
	patterns := strings.Split(permission.Action, ",")

	for _, action := range pkgPolicy.AllActions {
		if !collections.MatchItems(action, patterns...) {
			continue
		}

		policies = append(policies, createPolicy(permission, action))

		if objectSelector := rbacToABACObjectSelector(permission, action); objectSelector != nil {
			abacPermission := permission
			abacPermission.Object = ""
			abacPermission.ObjectSelector = objectSelector
			policies = append(policies, createPolicy(abacPermission, action))
		}
	}

	return policies
}

// createPolicy generates a Casbin policy rule from a permission.
func createPolicy(permission models.Permission, action string) []string {
	policy := []string{
		"p",
		permission.Principal(),
		permission.GetObject(),
		action,
		permission.Effect(),
		permission.Condition(),
		permission.ID.String(),
	}
	return policy
}

// rbacToABACObjectSelector returns object selectors (v1.PermissionObject) in JSON
// for ABAC policies from a global permission.
func rbacToABACObjectSelector(permission models.Permission, action string) []byte {
	switch permission.Object {
	case pkgPolicy.ObjectPlaybooks:
		if lo.Contains([]string{pkgPolicy.ActionPlaybookRun, pkgPolicy.ActionPlaybookApprove}, action) {
			return []byte(`{"playbooks": [{"name":"*"}]}`)
		}

	case pkgPolicy.ObjectCatalog:
		if pkgPolicy.ActionRead == action {
			return []byte(`{"configs": [{"name":"*"}]}`)
		}

	case pkgPolicy.ObjectTopology:
		if pkgPolicy.ActionRead == action {
			return []byte(`{"components": [{"name":"*"}]}`)
		}

	case pkgPolicy.ObjectConnection:
		if pkgPolicy.ActionRead == action {
			return []byte(`{"connections": [{"name":"*"}]}`)
		}
	}

	return nil
}

// Helper function for finding namespaced resources by selector
func (a *PermissionAdapter) findNamespacedResources(tableName string, selectors []v1.PermissionGroupSelector) ([]string, error) {
	var clauses []clause.Expression
	for _, selector := range selectors {
		if selector.Empty() {
			continue
		}

		var conditions = []clause.Expression{
			clause.Eq{Column: "deleted_at", Value: nil},
		}

		if !selector.Wildcard() {
			if selector.Namespace != "" {
				conditions = append(conditions, clause.Eq{Column: "namespace", Value: selector.Namespace})
			}
			if selector.Name != "" {
				conditions = append(conditions, clause.Eq{Column: "name", Value: selector.Name})
			}
		}

		clauses = append(clauses, clause.And(conditions...))
	}

	if len(clauses) == 0 {
		return nil, nil
	}

	var ids []string
	if err := a.db.Select("id").Table(tableName).Clauses(clause.Or(clauses...)).Find(&ids).Error; err != nil {
		return nil, err
	}

	return ids, nil
}

func (a *PermissionAdapter) permissionGroupToCasbinRule(permission models.PermissionGroup) ([][]string, error) {
	var subject v1.PermissionGroupSubjects
	if err := json.Unmarshal(permission.Selectors, &subject); err != nil {
		return nil, err
	}

	var allSubjects []string

	// Process namespaced resources
	namespacedSubjects := map[string][]v1.PermissionGroupSelector{
		"notifications":   subject.Notifications,
		"playbooks":       subject.Playbooks,
		"topologies":      subject.Topologies,
		"config_scrapers": subject.Scrapers,
		"canaries":        subject.Canaries,
	}

	for modelName, selectors := range namespacedSubjects {
		if len(selectors) == 0 {
			continue
		}

		ids, err := a.findNamespacedResources(modelName, selectors)
		if err != nil {
			return nil, fmt.Errorf("failed to find %s subjects for permission group %s: %w", modelName, permission.Name, err)
		}

		allSubjects = append(allSubjects, ids...)
	}

	if len(subject.People) > 0 {
		wildcard := len(subject.People) == 1 && subject.People[0] == "*"
		if wildcard {
			allSubjects = append(allSubjects, "everyone")
		} else {
			var personIDs []string
			query := a.db.Select("id").Model(&models.Person{}).
				Where("deleted_at IS NULL").
				Where("type IS DISTINCT FROM 'agent'").
				Where("email IS NOT NULL"). // Excludes system user
				Where("email IN ? OR name IN ?", subject.People, subject.People)
			if err := query.Find(&personIDs).Error; err != nil {
				return nil, err
			}

			allSubjects = append(allSubjects, personIDs...)
		}
	}

	if len(subject.Teams) > 0 {
		var teamIDs []string
		if err := a.db.Select("id").Model(&models.Team{}).Where("name = ?", subject.Teams).Find(&teamIDs).Error; err != nil {
			return nil, err
		}

		allSubjects = append(allSubjects, teamIDs...)
	}

	var policies [][]string
	for _, subject := range allSubjects {
		policy := []string{
			"g",
			subject,
			permission.Name,
			"",
			"",
			"",
		}

		policies = append(policies, policy)
	}

	return policies, nil
}

// expandPermissionScopes expands scope references in a permission's object_selector
// and returns a new permission with the expanded selectors merged in.
func (a *PermissionAdapter) expandPermissionScopes(perm models.Permission) (models.Permission, error) {
	// If no object selector, nothing to expand
	if len(perm.ObjectSelector) == 0 {
		return perm, nil
	}

	var selectors dutyRBAC.Selectors
	if err := json.Unmarshal(perm.ObjectSelector, &selectors); err != nil {
		return perm, fmt.Errorf("failed to unmarshal object_selector: %w", err)
	}

	// If no scope references, return as-is
	if len(selectors.Scopes) == 0 {
		return perm, nil
	}

	// Expand scopes and merge into selectors
	for _, scopeRef := range selectors.Scopes {
		var scope models.Scope
		err := a.db.
			Where("name = ? AND namespace = ? AND deleted_at IS NULL", scopeRef.Name, scopeRef.Namespace).
			First(&scope).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Log warning and skip - scope not found (similar to RLS behavior in auth/rls.go:147)
				continue
			}
			return perm, fmt.Errorf("failed to query scope %s/%s: %w", scopeRef.Namespace, scopeRef.Name, err)
		}

		var targets []v1.ScopeTarget
		if err := json.Unmarshal([]byte(scope.Targets), &targets); err != nil {
			// Log warning and skip - invalid JSON in scope targets (similar to RLS behavior in auth/rls.go:155)
			continue
		}

		// Merge targets into selectors (union approach)
		for _, target := range targets {
			if target.Config != nil {
				selectors.Configs = append(selectors.Configs, convertScopeResourceSelectorToResourceSelector(target.Config))
			}
			if target.Component != nil {
				selectors.Components = append(selectors.Components, convertScopeResourceSelectorToResourceSelector(target.Component))
			}
			if target.Playbook != nil {
				selectors.Playbooks = append(selectors.Playbooks, convertScopeResourceSelectorToResourceSelector(target.Playbook))
			}
			// Note: Canary targets are skipped - dutyRBAC.Selectors doesn't have a Canaries field
			// Note: Global targets are ignored per design decision
			// Note: Connection targets are ignored (no RLS support yet per auth/rls.go:127-132)
		}
	}

	// Marshal the expanded selectors back to JSON
	expandedObjectSelector, err := json.Marshal(selectors)
	if err != nil {
		return perm, fmt.Errorf("failed to marshal expanded selectors: %w", err)
	}

	// Create a new permission with the expanded object selector
	expandedPerm := perm
	expandedPerm.ObjectSelector = expandedObjectSelector

	return expandedPerm, nil
}

// convertScopeResourceSelectorToResourceSelector converts a v1.ScopeResourceSelector to types.ResourceSelector
func convertScopeResourceSelectorToResourceSelector(scopeSel *v1.ScopeResourceSelector) types.ResourceSelector {
	return types.ResourceSelector{
		Agent:       scopeSel.Agent,
		Name:        scopeSel.Name,
		TagSelector: scopeSel.TagSelector,
	}
}
