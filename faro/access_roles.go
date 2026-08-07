package main

import (
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

type accessRoleListOpts struct {
	Name  string `flag:"name" help:"Filter id or name with MatchItem patterns"`
	Type  string `flag:"type" help:"Filter role type with MatchItem patterns"`
	Limit int    `flag:"limit" help:"Maximum number of roles" default:"500"`
}

func listAccessRoles(opts accessRoleListOpts) ([]externalRole, error) {
	client, ctx, err := accessClient()
	if err != nil {
		return nil, err
	}
	roles, total, err := client.ListExternalRoles(ctx, sdk.IdentityOptions{Name: opts.Name, Type: opts.Type, Limit: opts.Limit})
	if err != nil {
		return nil, err
	}
	warnTruncated("roles", len(roles), total)
	return lo.Map(roles, func(r models.ExternalRole, _ int) externalRole { return externalRole{r} }), nil
}

type accessRoleGetFlags struct {
	Users  bool `flag:"users" help:"Show users holding this role" default:"true"`
	Groups bool `flag:"groups" help:"Show groups holding this role" default:"true"`
	All    bool `flag:"all" help:"Include every section (overrides individual flags)"`
}

func (accessRoleGetFlags) ClickyActionFlags() {}

func getAccessRole(id string, flags map[string]string) (any, error) {
	client, ctx, err := accessClient()
	if err != nil {
		return nil, err
	}
	role, err := client.ResolveExternalRole(ctx, id)
	if err != nil {
		return nil, err
	}

	all := clientcmd.BoolFlag(flags, "all", false)
	showUsers := all || clientcmd.BoolFlag(flags, "users", true)
	showGroups := all || clientcmd.BoolFlag(flags, "groups", true)

	result := &AccessRoleGetResult{Role: *role}
	if !showUsers && !showGroups {
		return result, nil
	}

	holders, err := client.GetRoleHolders(ctx, role.ID.String())
	if err != nil {
		return nil, err
	}
	if showUsers {
		result.Users = holders.Users
	}
	if showGroups {
		result.Groups = holders.Groups
	}
	return result, nil
}

// AccessRoleGetResult is the detailed view returned by `access roles get`.
type AccessRoleGetResult struct {
	Role   models.ExternalRole    `json:"role"`
	Users  []models.ExternalUser  `json:"users,omitempty"`
	Groups []models.ExternalGroup `json:"groups,omitempty"`
}

func (r AccessRoleGetResult) Pretty() api.Text {
	t := clicky.Text(r.Role.Name, "font-bold text-lg").NewLine().Append(externalRoleDetails(r.Role))

	if len(r.Users) > 0 {
		rows := lo.Map(r.Users, func(u models.ExternalUser, _ int) externalUserRow { return externalUserRow{u} })
		t = t.NewLine().Append(clicky.Collapsed(fmt.Sprintf("Users (%d)", len(rows)), api.NewTableFrom(rows)))
	}
	if len(r.Groups) > 0 {
		rows := lo.Map(r.Groups, func(g models.ExternalGroup, _ int) externalGroupRow { return externalGroupRow{g} })
		t = t.NewLine().Append(clicky.Collapsed(fmt.Sprintf("Groups (%d)", len(rows)), api.NewTableFrom(rows)))
	}
	return t
}

func completeAccessRoleIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	roles, err := listAccessRoles(accessRoleListOpts{Name: toComplete + "*", Limit: 20})
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
	return clientcmd.EchoFilterLookup(opts.Name), nil
}
func (accessRoleNameFilter) Options(_ accessRoleListOpts) map[string]api.Textable { return nil }

type accessRoleTypeFilter struct{}

func (accessRoleTypeFilter) Key() string   { return "type" }
func (accessRoleTypeFilter) Label() string { return "Type" }
func (accessRoleTypeFilter) Lookup(opts *accessRoleListOpts) (map[string]api.Textable, error) {
	return clientcmd.EchoFilterLookup(opts.Type), nil
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
