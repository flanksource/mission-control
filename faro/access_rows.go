package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

// humanSince renders an optional timestamp as an age, or a dash when unknown.
func humanSince(t *time.Time) any {
	if t == nil || t.IsZero() {
		return clicky.Text("-", "text-gray-400")
	}
	return api.Human(time.Since(*t), "text-gray-600")
}

func externalUserDetails(u models.ExternalUser) api.DescriptionList {
	items := []api.KeyValuePair{
		{Key: "ID", Value: u.ID.String()},
		{Key: "Type", Value: u.UserType},
	}
	if u.Email != nil && *u.Email != "" {
		items = append(items, api.KeyValuePair{Key: "Email", Value: *u.Email})
	}
	if u.Tenant != "" {
		items = append(items, api.KeyValuePair{Key: "Tenant", Value: u.Tenant})
	}
	if len(u.Aliases) > 0 {
		items = append(items, api.KeyValuePair{Key: "Aliases", Value: fmt.Sprintf("%v", []string(u.Aliases))})
	}
	if !u.CreatedAt.IsZero() {
		items = append(items, api.KeyValuePair{Key: "Created", Value: api.Human(time.Since(u.CreatedAt), "text-gray-600")})
	}
	return api.DescriptionList{Items: items}
}

func externalGroupDetails(g models.ExternalGroup) api.DescriptionList {
	items := []api.KeyValuePair{
		{Key: "ID", Value: g.ID.String()},
		{Key: "Type", Value: g.GroupType},
	}
	if g.Tenant != "" {
		items = append(items, api.KeyValuePair{Key: "Tenant", Value: g.Tenant})
	}
	if len(g.Aliases) > 0 {
		items = append(items, api.KeyValuePair{Key: "Aliases", Value: fmt.Sprintf("%v", []string(g.Aliases))})
	}
	if !g.CreatedAt.IsZero() {
		items = append(items, api.KeyValuePair{Key: "Created", Value: api.Human(time.Since(g.CreatedAt), "text-gray-600")})
	}
	return api.DescriptionList{Items: items}
}

func externalRoleDetails(role models.ExternalRole) api.DescriptionList {
	items := []api.KeyValuePair{
		{Key: "ID", Value: role.ID.String()},
		{Key: "Type", Value: role.RoleType},
	}
	if role.Description != "" {
		items = append(items, api.KeyValuePair{Key: "Description", Value: role.Description})
	}
	if role.Tenant != "" {
		items = append(items, api.KeyValuePair{Key: "Tenant", Value: role.Tenant})
	}
	if role.ApplicationID != nil {
		items = append(items, api.KeyValuePair{Key: "Application", Value: role.ApplicationID.String()})
	}
	if len(role.Aliases) > 0 {
		items = append(items, api.KeyValuePair{Key: "Aliases", Value: fmt.Sprintf("%v", []string(role.Aliases))})
	}
	return api.DescriptionList{Items: items}
}

// The identity models embed types.NoOpResourceSelectable, which clicky reflects
// into a column of its own in every format despite its `json:"-"` tag. Each
// listing therefore declares its columns explicitly rather than letting clicky
// derive them from the struct.

// externalUserRow renders a user as a table row, used by `users list` and
// `roles get`.
type externalUserRow struct {
	models.ExternalUser
}

func (r externalUserRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("Name").Build(),
		api.Column("Email").Build(),
		api.Column("UserType").Label("Type").Build(),
		api.Column("Tenant").Build(),
		api.Column("Created").Build(),
	}
}

func (r externalUserRow) Row() map[string]any {
	email := ""
	if r.Email != nil {
		email = *r.Email
	}
	return map[string]any{
		"Name":     clicky.Text(r.Name, "font-bold"),
		"Email":    clicky.Text(email, "text-gray-600"),
		"UserType": clicky.Text(r.UserType, "text-gray-500"),
		"Tenant":   clicky.Text(r.Tenant, "text-gray-600"),
		"Created":  humanSince(&r.CreatedAt),
	}
}

// externalGroupRow renders a group as a table row, used by `users get` and
// `roles get`.
type externalGroupRow struct {
	models.ExternalGroup
}

func (r externalGroupRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("Name").Build(),
		api.Column("GroupType").Label("Type").Build(),
		api.Column("Tenant").Build(),
	}
}

func (r externalGroupRow) Row() map[string]any {
	return map[string]any{
		"Name":      clicky.Text(r.Name, "font-bold"),
		"GroupType": clicky.Text(r.GroupType, "text-gray-500"),
		"Tenant":    clicky.Text(r.Tenant, "text-gray-600"),
	}
}

// externalGroupSummaryRow renders a group plus its member and permission counts,
// used by `groups list`.
type externalGroupSummaryRow struct {
	sdk.ExternalGroupSummary
}

func (r externalGroupSummaryRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("Name").Build(),
		api.Column("GroupType").Label("Type").Build(),
		api.Column("Tenant").Build(),
		api.Column("MembersCount").Label("Members").Build(),
		api.Column("PermissionsCount").Label("Permissions").Build(),
		api.Column("Created").Build(),
	}
}

func (r externalGroupSummaryRow) Row() map[string]any {
	return map[string]any{
		"Name":             clicky.Text(r.Name, "font-bold"),
		"GroupType":        clicky.Text(r.GroupType, "text-gray-500"),
		"Tenant":           clicky.Text(r.Tenant, "text-gray-600"),
		"MembersCount":     api.HumanNumber(int64(r.MembersCount), "text-gray-600"),
		"PermissionsCount": api.HumanNumber(int64(r.PermissionsCount), "font-bold"),
		"Created":          humanSince(&r.CreatedAt),
	}
}

// externalRole wraps models.ExternalRole, which has no GetID/GetName of its own,
// so it satisfies clicky.EntityItem. Used by `roles list`.
type externalRole struct {
	models.ExternalRole
}

func (r externalRole) GetID() string   { return r.ExternalRole.ID.String() }
func (r externalRole) GetName() string { return r.ExternalRole.Name }

func (r externalRole) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("Name").Build(),
		api.Column("RoleType").Label("Type").Build(),
		api.Column("Description").Build(),
		api.Column("Tenant").Build(),
		api.Column("Created").Build(),
	}
}

func (r externalRole) Row() map[string]any {
	return map[string]any{
		"Name":        clicky.Text(r.Name, "font-bold"),
		"RoleType":    clicky.Text(r.RoleType, "text-gray-500"),
		"Description": clicky.Text(r.Description, "text-gray-600"),
		"Tenant":      clicky.Text(r.Tenant, "text-gray-600"),
		"Created":     humanSince(&r.CreatedAt),
	}
}

// groupMemberRow renders one membership, flagging revoked memberships rather
// than hiding them so an audit can see removed access.
type groupMemberRow struct {
	sdk.GroupMember
}

func (r groupMemberRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("UserName").Label("User").Build(),
		api.Column("Email").Build(),
		api.Column("UserType").Label("Type").Build(),
		api.Column("Status").Build(),
		api.Column("LastSignedIn").Label("Last Signed In").Build(),
	}
}

func (r groupMemberRow) Row() map[string]any {
	status := clicky.Text("active", "text-green-600")
	if !r.Active() {
		status = clicky.Text("removed", "text-red-600")
	}
	return map[string]any{
		"UserName":     clicky.Text(r.UserName, "font-bold"),
		"Email":        clicky.Text(r.Email, "text-gray-600"),
		"UserType":     clicky.Text(r.UserType, "text-gray-500"),
		"Status":       status,
		"LastSignedIn": humanSince(r.LastSignedInAt),
	}
}

// accessGrantRow renders one (config, principal, role) grant.
//
// Unlike the server's access matrix row it leads with Config and Type: Pretty()
// groups by config, but --csv and --markdown flatten that grouping away, so
// without those columns the export cannot be attributed to a config.
type accessGrantRow struct {
	sdk.AccessGrant
}

func principalGrantRows(grants []sdk.AccessGrant) []accessGrantRow {
	return lo.Map(grants, func(g sdk.AccessGrant, _ int) accessGrantRow { return accessGrantRow{g} })
}

func (r accessGrantRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("ConfigName").Label("Config").Build(),
		api.Column("ConfigType").Label("Type").Build(),
		api.Column("User").Build(),
		api.Column("Email").Build(),
		api.Column("Role").Build(),
		api.Column("Source").Build(),
		api.Column("UserType").Label("User Type").Build(),
		api.Column("LastSignedIn").Label("Last Signed In").Build(),
	}
}

func (r accessGrantRow) Row() map[string]any {
	source := clicky.Text("direct", "text-gray-500")
	if r.GroupName != "" {
		source = clicky.Text("group:"+r.GroupName, "text-purple-600")
	}
	return map[string]any{
		"ConfigName":   clicky.Text(r.ConfigName, "font-bold"),
		"ConfigType":   clicky.Text(r.ConfigType, "text-gray-500"),
		"User":         clicky.Text(r.User, "font-bold"),
		"Email":        clicky.Text(r.Email, "text-gray-600"),
		"Role":         clicky.Text(r.Role),
		"Source":       source,
		"UserType":     clicky.Text(r.UserType, "text-gray-500"),
		"LastSignedIn": humanSince(r.LastSignedInAt),
	}
}

// AccessPermissionsResult is the top-level printable value for
// `access permissions`, the crosstab data behind the catalog report's access
// matrix. Pretty() groups by config; the flat rows survive --csv/--json.
type AccessPermissionsResult struct {
	Rows     []sdk.AccessGrant `json:"rows"`
	Expanded bool              `json:"expanded"`
}

func (r AccessPermissionsResult) Pretty() api.Text {
	if len(r.Rows) == 0 {
		return clicky.Text("No access entries found.", "text-gray-500")
	}

	byConfig := make(map[uuid.UUID][]sdk.AccessGrant)
	var order []uuid.UUID
	for _, row := range r.Rows {
		if _, ok := byConfig[row.ConfigID]; !ok {
			order = append(order, row.ConfigID)
		}
		byConfig[row.ConfigID] = append(byConfig[row.ConfigID], row)
	}

	sort.SliceStable(order, func(i, j int) bool {
		return byConfig[order[i]][0].ConfigName < byConfig[order[j]][0].ConfigName
	})

	t := clicky.Text(fmt.Sprintf("Access matrix: %d entries across %d configs", len(r.Rows), len(order)), "font-bold text-gray-700")
	if r.Expanded {
		t = t.AddText(" (expanded)", "text-xs text-gray-500")
	}

	for _, cid := range order {
		grants := byConfig[cid]
		label := fmt.Sprintf("%s (%s) — %d", grants[0].ConfigName, grants[0].ConfigType, len(grants))
		t = t.NewLine().Append(clicky.Collapsed(label, api.NewTableFrom(principalGrantRows(grants))))
	}
	return t
}

// accessLogRow renders one sign-in against a config.
type accessLogRow struct {
	sdk.AccessLog
}

func (r accessLogRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("ConfigName").Label("Config").Build(),
		api.Column("ConfigType").Label("Type").Build(),
		api.Column("User").Build(),
		api.Column("Email").Build(),
		api.Column("MFA").Build(),
		api.Column("Count").Build(),
		api.Column("SignedIn").Label("Signed In").Build(),
	}
}

func (r accessLogRow) Row() map[string]any {
	mfa := clicky.Text("no", "text-yellow-600")
	if r.MFA {
		mfa = clicky.Text("yes", "text-green-600")
	}
	count := 1
	if r.Count != nil {
		count = *r.Count
	}
	return map[string]any{
		"ConfigName": clicky.Text(r.ConfigName, "font-bold"),
		"ConfigType": clicky.Text(r.ConfigType, "text-gray-500"),
		"User":       clicky.Text(r.UserName(), "font-bold"),
		"Email":      clicky.Text(r.UserEmail(), "text-gray-600"),
		"MFA":        mfa,
		"Count":      api.HumanNumber(int64(count), "text-gray-600"),
		"SignedIn":   humanSince(&r.CreatedAt),
	}
}

// accessReviewRow renders one recorded review of a grant.
type accessReviewRow struct {
	sdk.AccessReview
}

func (r accessReviewRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("ConfigName").Label("Config").Build(),
		api.Column("ConfigType").Label("Type").Build(),
		api.Column("User").Build(),
		api.Column("Role").Build(),
		api.Column("Source").Build(),
		api.Column("Reviewed").Build(),
	}
}

func (r accessReviewRow) Row() map[string]any {
	return map[string]any{
		"ConfigName": clicky.Text(r.ConfigName, "font-bold"),
		"ConfigType": clicky.Text(r.ConfigType, "text-gray-500"),
		"User":       clicky.Text(r.User, "font-bold"),
		"Role":       clicky.Text(r.Role),
		"Source":     clicky.Text(r.Source, "text-gray-500"),
		"Reviewed":   humanSince(&r.CreatedAt),
	}
}
