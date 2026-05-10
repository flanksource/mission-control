package db

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyQuery "github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ListExternalUsers returns external users matching an optional case-insensitive
// name substring and an optional exact user_type. Soft-deleted users are
// excluded. Results are ordered by name.
func ListExternalUsers(ctx context.Context, name, userType string) (results []models.ExternalUser, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("ListExternalUsers").Arg("name", name).Arg("type", userType)
	defer timer.End(&err)

	q := ctx.DB().Table("external_users").Where("deleted_at IS NULL")
	if name != "" {
		q = q.Where("name ILIKE ? OR email ILIKE ?", "%"+name+"%", "%"+name+"%")
	}
	if userType != "" {
		q = q.Where("user_type = ?", userType)
	}
	if err = q.Order("name").Find(&results).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list external users")
	}
	timer.Results(results)
	return results, nil
}

// ListExternalGroups returns external groups matching an optional
// case-insensitive name substring and an optional exact group_type.
func ListExternalGroups(ctx context.Context, name, groupType string) (results []models.ExternalGroup, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("ListExternalGroups").Arg("name", name).Arg("type", groupType)
	defer timer.End(&err)

	q := ctx.DB().Table("external_groups").Where("deleted_at IS NULL")
	if name != "" {
		q = q.Where("name ILIKE ?", "%"+name+"%")
	}
	if groupType != "" {
		q = q.Where("group_type = ?", groupType)
	}
	if err = q.Order("name").Find(&results).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list external groups")
	}
	timer.Results(results)
	return results, nil
}

// ListExternalRoles returns external roles matching an optional
// case-insensitive name substring and an optional exact role_type.
func ListExternalRoles(ctx context.Context, name, roleType string) (results []models.ExternalRole, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("ListExternalRoles").Arg("name", name).Arg("type", roleType)
	defer timer.End(&err)

	q := ctx.DB().Table("external_roles").Where("deleted_at IS NULL")
	if name != "" {
		q = q.Where("name ILIKE ?", "%"+name+"%")
	}
	if roleType != "" {
		q = q.Where("role_type = ?", roleType)
	}
	if err = q.Order("name").Find(&results).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list external roles")
	}
	timer.Results(results)
	return results, nil
}

// GetAccessForUser returns every config the given user has direct access to.
// Group-mediated access is not included; use GetAccessForGroup per group and
// GetGroupsForUser to enumerate group memberships.
func GetAccessForUser(ctx context.Context, userID uuid.UUID) (results []RBACAccessRow, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("AccessForUser").Arg("userID", userID.String())
	defer timer.End(&err)

	err = rbacAccessBase(ctx).
		Where("config_access_summary.external_user_id = ?", userID).
		Order("config_access_summary.config_name").
		Find(&results).Error
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query access for user %s", userID)
	}
	timer.Results(results)
	return results, nil
}

// GetAccessForGroup returns every config the given group grants access to.
func GetAccessForGroup(ctx context.Context, groupID uuid.UUID) (results []RBACAccessRow, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("AccessForGroup").Arg("groupID", groupID.String())
	defer timer.End(&err)

	err = rbacAccessBase(ctx).
		Where("config_access_summary.external_group_id = ?", groupID).
		Order("config_access_summary.config_name").
		Find(&results).Error
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query access for group %s", groupID)
	}
	timer.Results(results)
	return results, nil
}

// GetGroupsForUser returns the active group memberships for a user.
func GetGroupsForUser(ctx context.Context, userID uuid.UUID) (results []models.ExternalGroup, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("GroupsForUser").Arg("userID", userID.String())
	defer timer.End(&err)

	err = ctx.DB().Table("external_groups").
		Joins("JOIN external_user_groups eug ON eug.external_group_id = external_groups.id").
		Where("eug.external_user_id = ?", userID).
		Where("eug.deleted_at IS NULL").
		Where("external_groups.deleted_at IS NULL").
		Order("external_groups.name").
		Find(&results).Error
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query groups for user %s", userID)
	}
	timer.Results(results)
	return results, nil
}

// GetGroupMembers returns the members of a specific group. Both active and
// soft-deleted memberships are returned so audit reviewers see recently
// removed users.
func GetGroupMembers(ctx context.Context, groupID uuid.UUID) (results []GroupMemberRow, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("GroupMembers").Arg("groupID", groupID.String())
	defer timer.End(&err)

	sql := `
		SELECT
			eg.id AS external_group_id,
			eg.name AS group_name,
			eg.group_type AS group_type,
			eu.id AS external_user_id,
			eu.name AS user_name,
			COALESCE(eu.email, '') AS email,
			eu.user_type AS user_type,
			last_sign_in.last_signed_in_at AS last_signed_in_at,
			eug.created_at AS membership_created_at,
			eug.deleted_at AS membership_deleted_at
		FROM external_user_groups eug
		JOIN external_groups eg ON eug.external_group_id = eg.id
		JOIN external_users eu ON eug.external_user_id = eu.id
		LEFT JOIN (
			SELECT external_user_id, MAX(created_at) AS last_signed_in_at
			FROM config_access_logs
			GROUP BY external_user_id
		) last_sign_in ON last_sign_in.external_user_id = eu.id
		WHERE eug.external_group_id = ?
		ORDER BY (eug.deleted_at IS NOT NULL) ASC, eu.name ASC`

	if err = ctx.DB().Raw(sql, groupID).Scan(&results).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query members for group %s", groupID)
	}
	timer.Results(results)
	return results, nil
}

// GetUsersForRole returns every user that currently holds the given role via a
// config_access entry.
func GetUsersForRole(ctx context.Context, roleID uuid.UUID) (results []models.ExternalUser, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("UsersForRole").Arg("roleID", roleID.String())
	defer timer.End(&err)

	err = ctx.DB().Table("external_users").
		Joins("JOIN config_access ca ON ca.external_user_id = external_users.id").
		Where("ca.external_role_id = ?", roleID).
		Where("ca.deleted_at IS NULL").
		Where("external_users.deleted_at IS NULL").
		Distinct("external_users.*").
		Order("external_users.name").
		Find(&results).Error
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query users for role %s", roleID)
	}
	timer.Results(results)
	return results, nil
}

// GetGroupsForRole returns every group that currently holds the given role via
// a config_access entry.
func GetGroupsForRole(ctx context.Context, roleID uuid.UUID) (results []models.ExternalGroup, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("GroupsForRole").Arg("roleID", roleID.String())
	defer timer.End(&err)

	err = ctx.DB().Table("external_groups").
		Joins("JOIN config_access ca ON ca.external_group_id = external_groups.id").
		Where("ca.external_role_id = ?", roleID).
		Where("ca.deleted_at IS NULL").
		Where("external_groups.deleted_at IS NULL").
		Distinct("external_groups.*").
		Order("external_groups.name").
		Find(&results).Error
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query groups for role %s", roleID)
	}
	timer.Results(results)
	return results, nil
}

// rbacAccessBase returns the shared SELECT + JOIN used by GetAccessForUser and
// GetAccessForGroup. Kept in sync with GetRBACAccess in rbac.go.
func rbacAccessBase(ctx context.Context) *gorm.DB {
	return ctx.DB().
		Table("config_access_summary").
		Select(`config_access_summary.config_id,
			config_access_summary.config_name,
			config_access_summary.config_type,
			config_access_summary.external_user_id,
			config_access_summary."user",
			config_access_summary.email,
			config_access_summary.role,
			config_access_summary.user_type,
			external_groups.name AS group_name,
			config_access_summary.created_at,
			config_access_summary.last_signed_in_at,
			config_access_summary.last_reviewed_at`).
		Joins("LEFT JOIN external_groups ON config_access_summary.external_group_id = external_groups.id")
}
