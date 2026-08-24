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

// accessUserListOpts drives `access users list` filtering.
type accessUserListOpts struct {
	Name  string `flag:"name" help:"Filter id, name, email or alias with MatchItem patterns"`
	Type  string `flag:"type" help:"Filter user type with MatchItem patterns"`
	Limit int    `flag:"limit" help:"Maximum number of users" default:"500"`
}

func listAccessUsers(opts accessUserListOpts) ([]externalUserRow, error) {
	client, ctx, err := accessClient()
	if err != nil {
		return nil, err
	}
	users, total, err := client.ListExternalUsers(ctx, sdk.IdentityOptions{Name: opts.Name, Type: opts.Type, Limit: opts.Limit})
	if err != nil {
		return nil, err
	}
	warnTruncated("users", len(users), total)
	return lo.Map(users, func(u models.ExternalUser, _ int) externalUserRow { return externalUserRow{u} }), nil
}

// accessUserGetFlags toggles the optional sections of `access users get`.
type accessUserGetFlags struct {
	Access bool `flag:"access" help:"Show configs the user has access to" default:"true"`
	Groups bool `flag:"groups" help:"Show groups the user belongs to" default:"true"`
	All    bool `flag:"all" help:"Include every section (overrides individual flags)"`
}

func (accessUserGetFlags) ClickyActionFlags() {}

func getAccessUser(id string, flags map[string]string) (any, error) {
	client, ctx, err := accessClient()
	if err != nil {
		return nil, err
	}
	user, err := client.ResolveExternalUser(ctx, id)
	if err != nil {
		return nil, err
	}

	all := clientcmd.BoolFlag(flags, "all", false)
	result := &AccessUserGetResult{User: *user}

	if all || clientcmd.BoolFlag(flags, "access", true) {
		grants, _, err := client.ListAccessGrants(ctx, sdk.AccessGrantOptions{UserIDs: []string{user.ID.String()}})
		if err != nil {
			return nil, err
		}
		result.Access = grants
	}
	if all || clientcmd.BoolFlag(flags, "groups", true) {
		groups, err := client.GetGroupsForUser(ctx, user.ID.String())
		if err != nil {
			return nil, err
		}
		result.Groups = groups
	}
	return result, nil
}

// AccessUserGetResult is the detailed view returned by `access users get`.
type AccessUserGetResult struct {
	User   models.ExternalUser    `json:"user"`
	Access []sdk.AccessGrant      `json:"access,omitempty"`
	Groups []models.ExternalGroup `json:"groups,omitempty"`
}

func (r AccessUserGetResult) Pretty() api.Text {
	t := clicky.Text(r.User.Name, "font-bold text-lg").NewLine().Append(externalUserDetails(r.User))

	if len(r.Groups) > 0 {
		rows := lo.Map(r.Groups, func(g models.ExternalGroup, _ int) externalGroupRow { return externalGroupRow{g} })
		t = t.NewLine().Append(clicky.Collapsed(fmt.Sprintf("Groups (%d)", len(rows)), api.NewTableFrom(rows)))
	}
	if len(r.Access) > 0 {
		t = t.NewLine().Append(clicky.Collapsed(fmt.Sprintf("Access (%d)", len(r.Access)), api.NewTableFrom(principalGrantRows(r.Access))))
	}
	return t
}

func completeAccessUserIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	users, err := listAccessUsers(accessUserListOpts{Name: toComplete + "*", Limit: 20})
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
	return clientcmd.EchoFilterLookup(opts.Name), nil
}
func (accessUserNameFilter) Options(_ accessUserListOpts) map[string]api.Textable { return nil }

type accessUserTypeFilter struct{}

func (accessUserTypeFilter) Key() string   { return "type" }
func (accessUserTypeFilter) Label() string { return "Type" }
func (accessUserTypeFilter) Lookup(opts *accessUserListOpts) (map[string]api.Textable, error) {
	return clientcmd.EchoFilterLookup(opts.Type), nil
}
func (accessUserTypeFilter) Options(_ accessUserListOpts) map[string]api.Textable { return nil }

func init() {
	clicky.RegisterEntity(clicky.Entity[externalUserRow, accessUserListOpts, any]{
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
