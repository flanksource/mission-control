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

type accessGroupListOpts struct {
	Name  string `flag:"name" help:"Filter id, name or alias with MatchItem patterns"`
	Type  string `flag:"type" help:"Filter group type with MatchItem patterns"`
	Limit int    `flag:"limit" help:"Maximum number of groups" default:"500"`
}

func listAccessGroups(opts accessGroupListOpts) ([]externalGroupSummaryRow, error) {
	client, ctx, err := accessClient()
	if err != nil {
		return nil, err
	}
	groups, total, err := client.ListExternalGroups(ctx, sdk.IdentityOptions{Name: opts.Name, Type: opts.Type, Limit: opts.Limit})
	if err != nil {
		return nil, err
	}
	warnTruncated("groups", len(groups), total)
	return lo.Map(groups, func(g sdk.ExternalGroupSummary, _ int) externalGroupSummaryRow {
		return externalGroupSummaryRow{g}
	}), nil
}

type accessGroupGetFlags struct {
	Members bool `flag:"members" help:"Show group members" default:"true"`
	Access  bool `flag:"access" help:"Show configs the group grants access to" default:"true"`
	All     bool `flag:"all" help:"Include every section (overrides individual flags)"`
}

func (accessGroupGetFlags) ClickyActionFlags() {}

func getAccessGroup(id string, flags map[string]string) (any, error) {
	client, ctx, err := accessClient()
	if err != nil {
		return nil, err
	}
	group, err := client.ResolveExternalGroup(ctx, id)
	if err != nil {
		return nil, err
	}

	all := clientcmd.BoolFlag(flags, "all", false)
	result := &AccessGroupGetResult{Group: *group}

	if all || clientcmd.BoolFlag(flags, "members", true) {
		members, err := client.GetGroupMembers(ctx, []string{group.ID.String()})
		if err != nil {
			return nil, err
		}
		result.Members = members
	}
	if all || clientcmd.BoolFlag(flags, "access", true) {
		grants, _, err := client.ListAccessGrants(ctx, sdk.AccessGrantOptions{GroupIDs: []string{group.ID.String()}})
		if err != nil {
			return nil, err
		}
		result.Access = grants
	}
	return result, nil
}

// AccessGroupGetResult is the detailed view returned by `access groups get`.
type AccessGroupGetResult struct {
	Group   models.ExternalGroup `json:"group"`
	Members []sdk.GroupMember    `json:"members,omitempty"`
	Access  []sdk.AccessGrant    `json:"access,omitempty"`
}

func (r AccessGroupGetResult) Pretty() api.Text {
	t := clicky.Text(r.Group.Name, "font-bold text-lg").NewLine().Append(externalGroupDetails(r.Group))

	if len(r.Members) > 0 {
		rows := lo.Map(r.Members, func(m sdk.GroupMember, _ int) groupMemberRow { return groupMemberRow{m} })
		t = t.NewLine().Append(clicky.Collapsed(fmt.Sprintf("Members (%d)", len(rows)), api.NewTableFrom(rows)))
	}
	if len(r.Access) > 0 {
		t = t.NewLine().Append(clicky.Collapsed(fmt.Sprintf("Access (%d)", len(r.Access)), api.NewTableFrom(principalGrantRows(r.Access))))
	}
	return t
}

func completeAccessGroupIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	groups, err := listAccessGroups(accessGroupListOpts{Name: toComplete + "*", Limit: 20})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.ID.String())
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

type accessGroupNameFilter struct{}

func (accessGroupNameFilter) Key() string   { return "name" }
func (accessGroupNameFilter) Label() string { return "Name" }
func (accessGroupNameFilter) Lookup(opts *accessGroupListOpts) (map[string]api.Textable, error) {
	return clientcmd.EchoFilterLookup(opts.Name), nil
}
func (accessGroupNameFilter) Options(_ accessGroupListOpts) map[string]api.Textable { return nil }

type accessGroupTypeFilter struct{}

func (accessGroupTypeFilter) Key() string   { return "type" }
func (accessGroupTypeFilter) Label() string { return "Type" }
func (accessGroupTypeFilter) Lookup(opts *accessGroupListOpts) (map[string]api.Textable, error) {
	return clientcmd.EchoFilterLookup(opts.Type), nil
}
func (accessGroupTypeFilter) Options(_ accessGroupListOpts) map[string]api.Textable { return nil }

func init() {
	clicky.RegisterEntity(clicky.Entity[externalGroupSummaryRow, accessGroupListOpts, any]{
		Name:   "groups",
		Parent: "access",
		Filters: []clicky.Filter[accessGroupListOpts]{
			accessGroupNameFilter{},
			accessGroupTypeFilter{},
		},
		List:         listAccessGroups,
		GetFlags:     accessGroupGetFlags{},
		GetWithFlags: getAccessGroup,
		ValidArgs:    completeAccessGroupIDs,
	})
}
