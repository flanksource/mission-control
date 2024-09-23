package adapter

import (
	"fmt"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/duty/models"
	"gorm.io/gorm"
)

type PermissionAdapter struct {
	*gormadapter.Adapter // gorm adapter for `casbin_rules` table

	db *gorm.DB
}

var _ persist.BatchAdapter = &PermissionAdapter{}

func NewPermissionAdapter(db *gorm.DB, main *gormadapter.Adapter) *PermissionAdapter {
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
	if err := a.db.Find(&permissions).Error; err != nil {
		return fmt.Errorf("failed to load permissions: %w", err)
	}

	for _, permission := range permissions {
		policy := permissionToCasbinRule(permission)
		if err := persist.LoadPolicyArray(policy, model); err != nil {
			return err
		}
	}

	return nil
}

func permissionToCasbinRule(permission models.Permission) []string {
	m := []string{
		"p",
		permission.Principal(),
		"", // the principal (v0) handles this
		permission.Action,
		permission.Effect(),
	}

	return m
}
