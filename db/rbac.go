package db

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"gorm.io/gorm"

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

func (r RBACAccessRow) RoleSource() string {
	if r.GroupName != nil && *r.GroupName != "" {
		return fmt.Sprintf("group:%s", *r.GroupName)
	}
	return "direct"
}

func GetRBACAccess(ctx context.Context, selectors []types.ResourceSelector) ([]RBACAccessRow, error) {
	query := ctx.DB().
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

	query = applyAccessSelectors(query, selectors)

	var rows []RBACAccessRow
	if err := query.
		Order("config_access_summary.config_name, config_access_summary.\"user\"").
		Find(&rows).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query RBAC access")
	}

	return rows, nil
}

func applyAccessSelectors(query *gorm.DB, selectors []types.ResourceSelector) *gorm.DB {
	if len(selectors) == 0 {
		return query
	}

	for _, s := range selectors {
		if s.ID != "" {
			query = query.Where("config_access_summary.config_id = ?", s.ID)
		}
		if len(s.Types) > 0 {
			query = query.Where("config_access_summary.config_type IN (?)", s.Types)
		}
		if s.Name != "" {
			query = query.Where("config_access_summary.config_name ILIKE ?", s.Name)
		}
		if s.Search != "" {
			pattern := "%" + s.Search + "%"
			query = query.Where("(config_access_summary.config_name ILIKE ? OR config_access_summary.config_type ILIKE ?)", pattern, pattern)
		}
	}

	return query
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
	ConfigID  uuid.UUID  `json:"config_id" gorm:"column:config_id"`
	UserName  string     `json:"user_name" gorm:"column:user_name"`
	UserEmail *string    `json:"user_email,omitempty" gorm:"column:user_email"`
	MFA       bool       `json:"mfa" gorm:"column:mfa"`
	Count     *int       `json:"count,omitempty" gorm:"column:count"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at"`
}

func GetAccessLogs(ctx context.Context, configID uuid.UUID, limit int) ([]AccessLogRow, error) {
	var rows []AccessLogRow
	err := ctx.DB().Raw(`
		SELECT cal.config_id, eu.name AS user_name, eu.email AS user_email,
			cal.mfa, cal.count, cal.created_at
		FROM config_access_logs cal
		JOIN external_users eu ON cal.external_user_id = eu.id
		WHERE cal.config_id = ?
		ORDER BY cal.created_at DESC
		LIMIT ?`, configID, limit).Scan(&rows).Error
	if err != nil {
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
	err := ctx.DB().Raw(`
		SELECT ar.config_id, ci.name AS config_name, ci.type AS config_type,
			COALESCE(eu.name, '') AS "user", COALESCE(er.name, '') AS role,
			COALESCE(ar.source, '') AS source, ar.created_at
		FROM access_reviews ar
		JOIN config_items ci ON ar.config_id = ci.id
		LEFT JOIN external_users eu ON ar.external_user_id = eu.id
		LEFT JOIN external_roles er ON ar.external_role_id = er.id
		WHERE (ar.config_id = ? OR ? IS NULL) AND ar.created_at >= ?
		ORDER BY ar.created_at DESC
		LIMIT ?`, configID, configID, since, limit).Scan(&rows).Error
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query access reviews")
	}
	return rows, nil
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours >= 24 {
		return fmt.Sprintf("%dd %dh", hours/24, hours%24)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}
