package adapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	pkgPolicy "github.com/flanksource/duty/rbac/policy"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		policies := PermissionToCasbinRule(permission)
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
	return []string{
		"p",
		permission.Principal(),
		permission.GetObject(),
		action,
		permission.Effect(),
		permission.Condition(),
		permission.ID.String(),
	}
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
