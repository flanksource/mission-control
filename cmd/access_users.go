package cmd

import (
	"fmt"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/db"
)

// accessUserListOpts drives `access users list` filtering.
type accessUserListOpts struct {
	Name string `flag:"name" help:"Filter by name or email substring (case-insensitive)"`
	Type string `flag:"type" help:"Filter by user type (e.g. Human, ServiceAccount)"`
}

func listAccessUsers(opts accessUserListOpts) ([]models.ExternalUser, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}
	return db.ListExternalUsers(ctx, opts.Name, opts.Type)
}

// accessUserGetFlags toggles the optional sections of `access users get`.
type accessUserGetFlags struct {
	Access bool `flag:"access" help:"Show configs the user has access to" default:"true"`
	Groups bool `flag:"groups" help:"Show groups the user belongs to" default:"true"`
	All    bool `flag:"all" help:"Include every section (overrides individual flags)"`
}

func (accessUserGetFlags) ClickyActionFlags() {}

func getAccessUser(id string, flags map[string]string) (any, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}
	user, err := resolveExternalUserArg(ctx, id)
	if err != nil {
		return nil, err
	}

	all := boolFlag(flags, "all", false)
	showAccess := all || boolFlag(flags, "access", true)
	showGroups := all || boolFlag(flags, "groups", true)

	result := &AccessUserGetResult{User: *user}
	if showAccess {
		rows, err := db.GetAccessForUser(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		result.Access = rows
	}
	if showGroups {
		groups, err := db.GetGroupsForUser(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		result.Groups = groups
	}
	return result, nil
}

// AccessUserGetResult is the detailed view returned by `access users get`.
type AccessUserGetResult struct {
	User   models.ExternalUser    `json:",inline"`
	Access []db.RBACAccessRow     `json:"access,omitempty"`
	Groups []models.ExternalGroup `json:"groups,omitempty"`
}

func (r AccessUserGetResult) Pretty() api.Text {
	t := clicky.Text(r.User.Name, "font-bold text-lg")
	t = t.NewLine().Append(buildUserDetails(r.User))

	if len(r.Groups) > 0 {
		rows := lo.Map(r.Groups, func(g models.ExternalGroup, _ int) externalGroupRow {
			return externalGroupRow{g}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Groups (%d)", len(rows)),
			api.NewTableFrom(rows),
		))
	}

	if len(r.Access) > 0 {
		rows := lo.Map(r.Access, func(row db.RBACAccessRow, _ int) accessMatrixRow {
			return accessMatrixRow{row}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Access (%d)", len(rows)),
			api.NewTableFrom(rows),
		))
	}

	return t
}

func buildUserDetails(u models.ExternalUser) api.DescriptionList {
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

// externalGroupRow is reused by `users get` and `roles get` to render a group
// as a table row.
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

// completeAccessUserIDs drives shell completion for the get/describe arg.
func completeAccessUserIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	users, err := listAccessUsers(accessUserListOpts{Name: toComplete})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.ID.String())
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

type accessUserNameFilter struct{}

func (accessUserNameFilter) Key() string   { return "name" }
func (accessUserNameFilter) Label() string { return "Name" }
func (accessUserNameFilter) Lookup(opts *accessUserListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Name), nil
}
func (accessUserNameFilter) Options(_ accessUserListOpts) map[string]api.Textable { return nil }

type accessUserTypeFilter struct{}

func (accessUserTypeFilter) Key() string   { return "type" }
func (accessUserTypeFilter) Label() string { return "Type" }
func (accessUserTypeFilter) Lookup(opts *accessUserListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Type), nil
}
func (accessUserTypeFilter) Options(_ accessUserListOpts) map[string]api.Textable { return nil }

func init() {
	clicky.RegisterEntity(clicky.Entity[models.ExternalUser, accessUserListOpts, any]{
		Name:   "users",
		Parent: "access",
		Filters: []clicky.Filter[accessUserListOpts]{
			accessUserNameFilter{},
			accessUserTypeFilter{},
		},
		List:         listAccessUsers,
		GetFlags:     accessUserGetFlags{},
		GetWithFlags: getAccessUser,
		ValidArgs:    completeAccessUserIDs,
	})
}
