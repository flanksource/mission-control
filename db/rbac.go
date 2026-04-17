package db

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	dutyQuery "github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
)

type RBACAccessRow struct {
	ConfigID       uuid.UUID  `gorm:"column:config_id"`
	ConfigName     string     `gorm:"column:config_name"`
	ConfigType     string     `gorm:"column:config_type"`
	UserID         uuid.UUID  `gorm:"column:external_user_id"`
	UserName       string     `gorm:"column:user"`
	Email          string     `gorm:"column:email"`
	Role           string     `gorm:"column:role"`
	UserType       string     `gorm:"column:user_type"`
	GroupName      *string    `gorm:"column:group_name"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	LastSignedInAt *time.Time `gorm:"column:last_signed_in_at"`
	LastReviewedAt *time.Time `gorm:"column:last_reviewed_at"`
}

func (r RBACAccessRow) QueryLogSummary() string {
	return r.ConfigType
}

func (r RBACAccessRow) RoleSource() string {
	if r.GroupName != nil && *r.GroupName != "" {
		return fmt.Sprintf("group:%s", *r.GroupName)
	}
	return "direct"
}

func GetRBACAccessByConfigIDs(ctx context.Context, configIDs []uuid.UUID) ([]RBACAccessRow, error) {
	return GetRBACAccess(ctx, nil, false, configIDs...)
}

func GetRBACAccess(ctx context.Context, selectors []types.ResourceSelector, recursive bool, configIDs ...uuid.UUID) (results []RBACAccessRow, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("RBACAccess").Arg("configIDs", len(configIDs)).Arg("selectors", len(selectors))
	defer timer.End(&err)

	q := ctx.DB().
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

	if len(selectors) > 0 {
		resolved, err := dutyQuery.FindConfigIDsByResourceSelector(ctx, 0, selectors...)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to resolve config selectors")
		}
		if len(resolved) == 0 {
			return nil, nil
		}
		if recursive {
			resolved, err = ExpandConfigChildren(ctx, resolved)
			if err != nil {
				return nil, ctx.Oops().Wrapf(err, "failed to expand children")
			}
		}
		configIDs = append(configIDs, resolved...)
	}

	if len(configIDs) > 0 {
		q = q.Where("config_access_summary.config_id IN (?)", configIDs)
	}

	if err = q.
		Order("config_access_summary.config_name, config_access_summary.\"user\"").
		Find(&results).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query RBAC access")
	}
	timer.Results(results)
	return results, nil
}

func ExpandConfigChildren(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	return dutyQuery.ExpandConfigChildren(ctx, ids)
}

// GroupMemberRow represents one (group, user) pair — the membership of an
// external user in an external group. Used by the catalog report audit page
// to enumerate who is in each group that grants access to reported configs.
type GroupMemberRow struct {
	GroupID             uuid.UUID  `gorm:"column:external_group_id"`
	GroupName           string     `gorm:"column:group_name"`
	GroupType           string     `gorm:"column:group_type"`
	UserID              uuid.UUID  `gorm:"column:external_user_id"`
	UserName            string     `gorm:"column:user_name"`
	Email               string     `gorm:"column:email"`
	UserType            string     `gorm:"column:user_type"`
	LastSignedInAt      *time.Time `gorm:"column:last_signed_in_at"`
	MembershipAddedAt   time.Time  `gorm:"column:membership_created_at"`
	MembershipDeletedAt *time.Time `gorm:"column:membership_deleted_at"`
}

func (r GroupMemberRow) QueryLogSummary() string {
	return r.GroupName
}

// GetGroupMembersForConfigs returns the members of every external group that
// is referenced by an active config_access row on any of the given configs.
// Both active and soft-deleted group memberships are returned so that audit
// reviewers can see users who were recently removed from a group.
func GetGroupMembersForConfigs(ctx context.Context, configIDs []uuid.UUID) (results []GroupMemberRow, err error) {
	timer := dutyQuery.NewQueryLogger(ctx).Start("GroupMembers").Arg("configIDs", len(configIDs))
	defer timer.End(&err)

	if len(configIDs) == 0 {
		return nil, nil
	}

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
		WHERE eug.external_group_id IN (
			SELECT DISTINCT external_group_id
			FROM config_access
			WHERE config_id IN (?)
				AND external_group_id IS NOT NULL
				AND deleted_at IS NULL
		)
		ORDER BY eg.name ASC,
			(eug.deleted_at IS NOT NULL) ASC,
			eu.name ASC`

	if err = ctx.DB().Raw(sql, configIDs).Scan(&results).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query group members for configs")
	}
	timer.Results(results)
	return results, nil
}

func GetRBACChangelog(ctx context.Context, configIDs []uuid.UUID, since time.Time) ([]api.RBACChangeEntry, error) {
	if len(configIDs) == 0 {
		return nil, nil
	}

	grantsSQL := `
		SELECT
			ca.config_id::text AS config_id,
			ca.created_at AS date,
			'PermissionGranted' AS change_type,
			COALESCE(eu.name, '') AS "user",
			COALESCE(er.name, '') AS role,
			COALESCE(ci.name, '') AS config_name,
			COALESCE(ca.source, '') AS source,
			'Access granted' AS description
		FROM config_access ca
		JOIN config_items ci ON ca.config_id = ci.id
		LEFT JOIN external_users eu ON ca.external_user_id = eu.id
		LEFT JOIN external_roles er ON ca.external_role_id = er.id
		WHERE ca.config_id IN (?) AND ca.created_at >= ?`

	revocationsSQL := `
		SELECT
			ca.config_id::text AS config_id,
			ca.deleted_at AS date,
			'PermissionRevoked' AS change_type,
			COALESCE(eu.name, '') AS "user",
			COALESCE(er.name, '') AS role,
			COALESCE(ci.name, '') AS config_name,
			COALESCE(ca.source, '') AS source,
			'Access revoked' AS description
		FROM config_access ca
		JOIN config_items ci ON ca.config_id = ci.id
		LEFT JOIN external_users eu ON ca.external_user_id = eu.id
		LEFT JOIN external_roles er ON ca.external_role_id = er.id
		WHERE ca.config_id IN (?) AND ca.deleted_at IS NOT NULL AND ca.deleted_at >= ?`

	reviewsSQL := `
		SELECT
			ar.config_id::text AS config_id,
			ar.created_at AS date,
			'AccessReviewed' AS change_type,
			COALESCE(eu.name, '') AS "user",
			COALESCE(er.name, '') AS role,
			COALESCE(ci.name, '') AS config_name,
			COALESCE(ar.source, '') AS source,
			'Access reviewed' AS description
		FROM access_reviews ar
		JOIN config_items ci ON ar.config_id = ci.id
		LEFT JOIN external_users eu ON ar.external_user_id = eu.id
		LEFT JOIN external_roles er ON ar.external_role_id = er.id
		WHERE ar.config_id IN (?) AND ar.created_at >= ?`

	// Exclude temporary access (< 72h) from changelog
	tempFilter := ` AND NOT (ca.deleted_at IS NOT NULL AND EXTRACT(EPOCH FROM ca.deleted_at - ca.created_at) < 259200)`
	grantsSQL += tempFilter
	revocationsSQL += tempFilter

	unionSQL := fmt.Sprintf("(%s) UNION ALL (%s) UNION ALL (%s) ORDER BY date DESC",
		grantsSQL, revocationsSQL, reviewsSQL)

	var entries []api.RBACChangeEntry
	if err := ctx.DB().Raw(unionSQL,
		configIDs, since,
		configIDs, since,
		configIDs, since,
	).Scan(&entries).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query RBAC changelog")
	}

	return entries, nil
}

func GetRBACTemporaryAccess(ctx context.Context, configIDs []uuid.UUID, since time.Time) ([]api.RBACTemporaryAccess, error) {
	if len(configIDs) == 0 {
		return nil, nil
	}

	sql := `
		SELECT
			ca.config_id::text AS config_id,
			COALESCE(eu.name, '') AS "user",
			COALESCE(er.name, '') AS role,
			COALESCE(ca.source, '') AS source,
			ca.created_at AS granted_at,
			ca.deleted_at AS revoked_at
		FROM config_access ca
		JOIN config_items ci ON ca.config_id = ci.id
		LEFT JOIN external_users eu ON ca.external_user_id = eu.id
		LEFT JOIN external_roles er ON ca.external_role_id = er.id
		WHERE ca.config_id IN (?)
			AND ca.deleted_at IS NOT NULL
			AND ca.created_at >= ?
			AND EXTRACT(EPOCH FROM ca.deleted_at - ca.created_at) < 259200
		ORDER BY ca.created_at DESC`

	type row struct {
		ConfigID  string    `gorm:"column:config_id"`
		User      string    `gorm:"column:user"`
		Role      string    `gorm:"column:role"`
		Source    string    `gorm:"column:source"`
		GrantedAt time.Time `gorm:"column:granted_at"`
		RevokedAt time.Time `gorm:"column:revoked_at"`
	}

	var rows []row
	if err := ctx.DB().Raw(sql, configIDs, since).Scan(&rows).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query temporary access")
	}

	results := make([]api.RBACTemporaryAccess, len(rows))
	for i, r := range rows {
		results[i] = api.RBACTemporaryAccess{
			ConfigID:  r.ConfigID,
			User:      r.User,
			Role:      r.Role,
			Source:    r.Source,
			GrantedAt: r.GrantedAt,
			RevokedAt: r.RevokedAt,
			Duration:  formatDuration(r.RevokedAt.Sub(r.GrantedAt)),
		}
	}
	return results, nil
}

type AccessLogRow struct {
	ConfigID  uuid.UUID `json:"config_id" gorm:"column:config_id"`
	UserName  string    `json:"user_name" gorm:"column:user_name"`
	UserEmail *string   `json:"user_email,omitempty" gorm:"column:user_email"`
	MFA       bool      `json:"mfa" gorm:"column:mfa"`
	Count     *int      `json:"count,omitempty" gorm:"column:count"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
}

func GetAccessLogs(ctx context.Context, configID uuid.UUID, userID *uuid.UUID, limit int) ([]AccessLogRow, error) {
	q := ctx.DB().Table("config_access_logs cal").
		Select(`cal.config_id, eu.name AS user_name, eu.email AS user_email,
			cal.mfa, cal.count, cal.created_at`).
		Joins("JOIN external_users eu ON cal.external_user_id = eu.id").
		Where("cal.config_id = ?", configID).
		Order("cal.created_at DESC").
		Limit(limit)

	if userID != nil {
		q = q.Where("cal.external_user_id = ?", *userID)
	}

	var rows []AccessLogRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query access logs")
	}
	return rows, nil
}

type AccessReviewRow struct {
	ConfigID   uuid.UUID `json:"config_id" gorm:"column:config_id"`
	ConfigName string    `json:"config_name" gorm:"column:config_name"`
	ConfigType string    `json:"config_type" gorm:"column:config_type"`
	User       string    `json:"user" gorm:"column:user"`
	Role       string    `json:"role" gorm:"column:role"`
	Source     string    `json:"source" gorm:"column:source"`
	CreatedAt  time.Time `json:"created_at" gorm:"column:created_at"`
}

func GetAccessReviews(ctx context.Context, configID *uuid.UUID, since time.Time, limit int) ([]AccessReviewRow, error) {
	var rows []AccessReviewRow
	q := ctx.DB().Table("access_reviews ar").
		Select(`ar.config_id, ci.name AS config_name, ci.type AS config_type,
			COALESCE(eu.name, '') AS "user", COALESCE(er.name, '') AS role,
			COALESCE(ar.source, '') AS source, ar.created_at`).
		Joins("JOIN config_items ci ON ar.config_id = ci.id").
		Joins("LEFT JOIN external_users eu ON ar.external_user_id = eu.id").
		Joins("LEFT JOIN external_roles er ON ar.external_role_id = er.id").
		Where("ar.created_at >= ?", since).
		Order("ar.created_at DESC").
		Limit(limit)
	if configID != nil {
		q = q.Where("ar.config_id = ?", *configID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query access reviews")
	}
	return rows, nil
}

func GetPermissionSubjects(ctx context.Context) ([]string, error) {
	var subjects []string
	if err := ctx.DB().
		Table("permission_subjects").
		Distinct("id").
		Where("id IS NOT NULL").
		Where("id != ''").
		Order("id").
		Pluck("id", &subjects).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query permission subjects")
	}

	return subjects, nil
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours >= 24 {
		return fmt.Sprintf("%dd %dh", hours/24, hours%24)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}
