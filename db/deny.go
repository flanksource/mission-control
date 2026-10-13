package db

import (
	"github.com/flanksource/duty/context"
)

// DeleteDenyRules deletes every Role stored with a deny rule, and every Permission stored with deny set.
// Rules and Permissions only allow: read without deny, each would allow what it was written to refuse.
//
// Permissions are deleted only while the deny column exists: duty's migration drops it, deleting them too.
func DeleteDenyRules(ctx context.Context) error {
	var roles []struct{ Namespace, Name string }
	if err := ctx.DB().Raw(`
		UPDATE roles SET deleted_at = NOW()
		WHERE deleted_at IS NULL AND jsonb_path_exists(rules, '$[*] ? (@.deny == true)')
		RETURNING namespace, name`).Scan(&roles).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to delete roles with deny rules")
	}
	for _, role := range roles {
		ctx.Warnf("deleted role %s/%s: it has a deny rule, and rules only allow", role.Namespace, role.Name)
	}

	var hasDeny bool
	if err := ctx.DB().Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'permissions' AND column_name = 'deny')`).Scan(&hasDeny).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to check for the deny column of permissions")
	} else if !hasDeny {
		return nil
	}

	var permissions []struct{ Namespace, Name string }
	if err := ctx.DB().Raw(`
		UPDATE permissions SET deleted_at = NOW()
		WHERE deleted_at IS NULL AND deny
		RETURNING COALESCE(namespace, '') AS namespace, COALESCE(name, '') AS name`).Scan(&permissions).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to delete deny permissions")
	}
	for _, permission := range permissions {
		ctx.Warnf("deleted permission %s/%s: it denies, and permissions only allow", permission.Namespace, permission.Name)
	}

	return nil
}
