package cmd

import (
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/db"
)

type accessRoleListOpts struct {
	Name string `flag:"name" help:"Filter by name substring (case-insensitive)"`
	Type string `flag:"type" help:"Filter by role type"`
}

// externalRole wraps models.ExternalRole so it satisfies clicky.EntityItem.
type externalRole struct {
	models.ExternalRole
}

func (r externalRole) GetID() string   { return r.ExternalRole.ID.String() }
func (r externalRole) GetName() string { return r.ExternalRole.Name }

func listAccessRoles(opts accessRoleListOpts) ([]externalRole, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}
	roles, err := db.ListExternalRoles(ctx, opts.Name, opts.Type)
	if err != nil {
		return nil, err
	}
	out := make([]externalRole, len(roles))
	for i, r := range roles {
		out[i] = externalRole{r}
	}
	return out, nil
}

type accessRoleGetFlags struct {
	Users  bool `flag:"users" help:"Show users holding this role" default:"true"`
	Groups bool `flag:"groups" help:"Show groups holding this role" default:"true"`
	All    bool `flag:"all" help:"Include every section (overrides individual flags)"`
}

func (accessRoleGetFlags) ClickyActionFlags() {}

func getAccessRole(id string, flags map[string]string) (any, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}
	role, err := resolveExternalRoleArg(ctx, id)
	if err != nil {
		return nil, err
	}

	all := boolFlag(flags, "all", false)
	showUsers := all || boolFlag(flags, "users", true)
	showGroups := all || boolFlag(flags, "groups", true)

	result := &AccessRoleGetResult{Role: *role}
	if showUsers {
		users, err := db.GetUsersForRole(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		result.Users = users
	}
	if showGroups {
		groups, err := db.GetGroupsForRole(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		result.Groups = groups
	}
	return result, nil
}

// AccessRoleGetResult is the detailed view returned by `access roles get`.
type AccessRoleGetResult struct {
	Role   models.ExternalRole    `json:",inline"`
	Users  []models.ExternalUser  `json:"users,omitempty"`
	Groups []models.ExternalGroup `json:"groups,omitempty"`
}

func (r AccessRoleGetResult) Pretty() api.Text {
	t := clicky.Text(r.Role.Name, "font-bold text-lg")
	t = t.NewLine().Append(buildRoleDetails(r.Role))

	if len(r.Users) > 0 {
		rows := lo.Map(r.Users, func(u models.ExternalUser, _ int) externalUserRow {
			return externalUserRow{u}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Users (%d)", len(rows)),
			api.NewTableFrom(rows),
		))
	}

	if len(r.Groups) > 0 {
		rows := lo.Map(r.Groups, func(g models.ExternalGroup, _ int) externalGroupRow {
			return externalGroupRow{g}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Groups (%d)", len(rows)),
			api.NewTableFrom(rows),
		))
	}

	return t
}

func buildRoleDetails(role models.ExternalRole) api.DescriptionList {
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

// externalUserRow exposes models.ExternalUser as a clicky table row, used by
// roles get to show users that hold a role.
type externalUserRow struct {
	models.ExternalUser
}

func (r externalUserRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("Name").Build(),
		api.Column("Email").Build(),
		api.Column("UserType").Label("Type").Build(),
		api.Column("Tenant").Build(),
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
	}
}

func completeAccessRoleIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	roles, err := listAccessRoles(accessRoleListOpts{Name: toComplete})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.GetID())
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

type accessRoleNameFilter struct{}

func (accessRoleNameFilter) Key() string   { return "name" }
func (accessRoleNameFilter) Label() string { return "Name" }
func (accessRoleNameFilter) Lookup(opts *accessRoleListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Name), nil
}
func (accessRoleNameFilter) Options(_ accessRoleListOpts) map[string]api.Textable { return nil }

type accessRoleTypeFilter struct{}

func (accessRoleTypeFilter) Key() string   { return "type" }
func (accessRoleTypeFilter) Label() string { return "Type" }
func (accessRoleTypeFilter) Lookup(opts *accessRoleListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Type), nil
}
func (accessRoleTypeFilter) Options(_ accessRoleListOpts) map[string]api.Textable { return nil }

func init() {
	clicky.RegisterEntity(clicky.Entity[externalRole, accessRoleListOpts, any]{
		Name:   "roles",
		Parent: "access",
		Filters: []clicky.Filter[accessRoleListOpts]{
			accessRoleNameFilter{},
			accessRoleTypeFilter{},
		},
		List:         listAccessRoles,
		GetFlags:     accessRoleGetFlags{},
		GetWithFlags: getAccessRole,
		ValidArgs:    completeAccessRoleIDs,
	})
}
