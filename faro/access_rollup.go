package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

// AccessSummaryByUserResult is the printable value for
// `access permissions --by-user`. Derived marks a rollup computed client-side
// from filtered grant rows rather than read from config_access_summary_by_user,
// which cannot be narrowed.
type AccessSummaryByUserResult struct {
	Rows    []sdk.AccessSummaryByUser `json:"rows"`
	Derived bool                      `json:"derived"`
}

func (r AccessSummaryByUserResult) Pretty() api.Text {
	if len(r.Rows) == 0 {
		return clicky.Text("No access entries found.", "text-gray-500")
	}
	t := clicky.Text(fmt.Sprintf("Access by user: %d users", len(r.Rows)), "font-bold text-gray-700")
	if r.Derived {
		t = t.AddText(" (filtered)", "text-xs text-gray-500")
	}
	return t.NewLine().Append(api.NewTableFrom(userSummaryRows(r.Rows)))
}

func userSummaryRows(rows []sdk.AccessSummaryByUser) []accessSummaryByUserRow {
	return lo.Map(rows, func(row sdk.AccessSummaryByUser, _ int) accessSummaryByUserRow {
		return accessSummaryByUserRow{row}
	})
}

// AccessSummaryByConfigResult is the printable value for
// `access permissions --by-config`.
type AccessSummaryByConfigResult struct {
	Rows    []sdk.AccessSummaryByConfig `json:"rows"`
	Derived bool                        `json:"derived"`
}

func (r AccessSummaryByConfigResult) Pretty() api.Text {
	if len(r.Rows) == 0 {
		return clicky.Text("No access entries found.", "text-gray-500")
	}
	t := clicky.Text(fmt.Sprintf("Access by config: %d configs", len(r.Rows)), "font-bold text-gray-700")
	if r.Derived {
		t = t.AddText(" (filtered)", "text-xs text-gray-500")
	}
	return t.NewLine().Append(api.NewTableFrom(configSummaryRows(r.Rows)))
}

func configSummaryRows(rows []sdk.AccessSummaryByConfig) []accessSummaryByConfigRow {
	return lo.Map(rows, func(row sdk.AccessSummaryByConfig, _ int) accessSummaryByConfigRow {
		return accessSummaryByConfigRow{row}
	})
}

type accessSummaryByUserRow struct {
	sdk.AccessSummaryByUser
}

func (r accessSummaryByUserRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("User").Build(),
		api.Column("Email").Build(),
		api.Column("DistinctConfigs").Label("Configs").Build(),
		api.Column("DistinctRoles").Label("Roles").Build(),
		api.Column("AccessCount").Label("Grants").Build(),
		api.Column("LastSignedIn").Label("Last Signed In").Build(),
		api.Column("LatestGrant").Label("Latest Grant").Build(),
	}
}

func (r accessSummaryByUserRow) Row() map[string]any {
	return map[string]any{
		"User":            clicky.Text(r.User, "font-bold"),
		"Email":           clicky.Text(r.Email, "text-gray-600"),
		"DistinctConfigs": api.HumanNumber(int64(r.DistinctConfigs), "text-gray-600"),
		"DistinctRoles":   api.HumanNumber(int64(r.DistinctRoles), "text-gray-600"),
		"AccessCount":     api.HumanNumber(int64(r.AccessCount), "font-bold"),
		"LastSignedIn":    humanSince(r.LastSignedInAt),
		"LatestGrant":     humanSince(r.LatestGrant),
	}
}

type accessSummaryByConfigRow struct {
	sdk.AccessSummaryByConfig
}

func (r accessSummaryByConfigRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("ConfigName").Label("Config").Build(),
		api.Column("ConfigType").Label("Type").Build(),
		api.Column("DistinctUsers").Label("Users").Build(),
		api.Column("DistinctRoles").Label("Roles").Build(),
		api.Column("AccessCount").Label("Grants").Build(),
		api.Column("LastSignedIn").Label("Last Signed In").Build(),
		api.Column("LatestGrant").Label("Latest Grant").Build(),
	}
}

func (r accessSummaryByConfigRow) Row() map[string]any {
	return map[string]any{
		"ConfigName":    clicky.Text(r.ConfigName, "font-bold"),
		"ConfigType":    clicky.Text(r.ConfigType, "text-gray-500"),
		"DistinctUsers": api.HumanNumber(int64(r.DistinctUsers), "text-gray-600"),
		"DistinctRoles": api.HumanNumber(int64(r.DistinctRoles), "text-gray-600"),
		"AccessCount":   api.HumanNumber(int64(r.AccessCount), "font-bold"),
		"LastSignedIn":  humanSince(r.LastSignedInAt),
		"LatestGrant":   humanSince(r.LatestGrant),
	}
}

// rollupByUser aggregates grant rows the same way config_access_summary_by_user
// does, for the filtered case where that view cannot be used.
func rollupByUser(grants []sdk.AccessGrant) []sdk.AccessSummaryByUser {
	type bucket struct {
		row     sdk.AccessSummaryByUser
		roles   map[string]struct{}
		configs map[uuid.UUID]struct{}
	}

	buckets := map[uuid.UUID]*bucket{}
	var order []uuid.UUID
	for _, g := range grants {
		b, ok := buckets[g.ExternalUserID]
		if !ok {
			b = &bucket{
				row:     sdk.AccessSummaryByUser{ExternalUserID: g.ExternalUserID, User: g.User, Email: g.Email},
				roles:   map[string]struct{}{},
				configs: map[uuid.UUID]struct{}{},
			}
			buckets[g.ExternalUserID] = b
			order = append(order, g.ExternalUserID)
		}
		b.row.AccessCount++
		b.row.LastSignedInAt = later(b.row.LastSignedInAt, g.LastSignedInAt)
		b.row.LatestGrant = later(b.row.LatestGrant, &g.CreatedAt)
		b.roles[g.Role] = struct{}{}
		b.configs[g.ConfigID] = struct{}{}
	}

	out := make([]sdk.AccessSummaryByUser, 0, len(order))
	for _, id := range order {
		b := buckets[id]
		b.row.DistinctRoles = len(b.roles)
		b.row.DistinctConfigs = len(b.configs)
		out = append(out, b.row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AccessCount != out[j].AccessCount {
			return out[i].AccessCount > out[j].AccessCount
		}
		return out[i].User < out[j].User
	})
	return out
}

// rollupByConfig aggregates grant rows the same way
// config_access_summary_by_config does.
func rollupByConfig(grants []sdk.AccessGrant) []sdk.AccessSummaryByConfig {
	type bucket struct {
		row   sdk.AccessSummaryByConfig
		roles map[string]struct{}
		users map[uuid.UUID]struct{}
	}

	buckets := map[uuid.UUID]*bucket{}
	var order []uuid.UUID
	for _, g := range grants {
		b, ok := buckets[g.ConfigID]
		if !ok {
			b = &bucket{
				row:   sdk.AccessSummaryByConfig{ConfigID: g.ConfigID, ConfigName: g.ConfigName, ConfigType: g.ConfigType},
				roles: map[string]struct{}{},
				users: map[uuid.UUID]struct{}{},
			}
			buckets[g.ConfigID] = b
			order = append(order, g.ConfigID)
		}
		b.row.AccessCount++
		b.row.LastSignedInAt = later(b.row.LastSignedInAt, g.LastSignedInAt)
		b.row.LatestGrant = later(b.row.LatestGrant, &g.CreatedAt)
		b.roles[g.Role] = struct{}{}
		b.users[g.ExternalUserID] = struct{}{}
	}

	out := make([]sdk.AccessSummaryByConfig, 0, len(order))
	for _, id := range order {
		b := buckets[id]
		b.row.DistinctRoles = len(b.roles)
		b.row.DistinctUsers = len(b.users)
		out = append(out, b.row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AccessCount != out[j].AccessCount {
			return out[i].AccessCount > out[j].AccessCount
		}
		return out[i].ConfigName < out[j].ConfigName
	})
	return out
}

// later returns the more recent of two optional timestamps, copying the winner
// so the result never aliases a caller's field.
func later(current, candidate *time.Time) *time.Time {
	if candidate == nil || candidate.IsZero() {
		return current
	}
	if current == nil || candidate.After(*current) {
		v := *candidate
		return &v
	}
	return current
}
