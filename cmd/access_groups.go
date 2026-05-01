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

type accessGroupListOpts struct {
	Name string `flag:"name" help:"Filter by name substring (case-insensitive)"`
	Type string `flag:"type" help:"Filter by group type"`
}

func listAccessGroups(opts accessGroupListOpts) ([]models.ExternalGroup, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}
	return db.ListExternalGroups(ctx, opts.Name, opts.Type)
}

type accessGroupGetFlags struct {
	Members bool `flag:"members" help:"Show group members" default:"true"`
	Access  bool `flag:"access" help:"Show configs the group grants access to" default:"true"`
	All     bool `flag:"all" help:"Include every section (overrides individual flags)"`
}

func (accessGroupGetFlags) ClickyActionFlags() {}

func getAccessGroup(id string, flags map[string]string) (any, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}
	group, err := resolveExternalGroupArg(ctx, id)
	if err != nil {
		return nil, err
	}

	all := boolFlag(flags, "all", false)
	showMembers := all || boolFlag(flags, "members", true)
	showAccess := all || boolFlag(flags, "access", true)

	result := &AccessGroupGetResult{Group: *group}
	if showMembers {
		members, err := db.GetGroupMembers(ctx, group.ID)
		if err != nil {
			return nil, err
		}
		result.Members = members
	}
	if showAccess {
		rows, err := db.GetAccessForGroup(ctx, group.ID)
		if err != nil {
			return nil, err
		}
		result.Access = rows
	}
	return result, nil
}

// AccessGroupGetResult is the detailed view returned by `access groups get`.
type AccessGroupGetResult struct {
	Group   models.ExternalGroup `json:",inline"`
	Members []db.GroupMemberRow  `json:"members,omitempty"`
	Access  []db.RBACAccessRow   `json:"access,omitempty"`
}

func (r AccessGroupGetResult) Pretty() api.Text {
	t := clicky.Text(r.Group.Name, "font-bold text-lg")
	t = t.NewLine().Append(buildGroupDetails(r.Group))

	if len(r.Members) > 0 {
		rows := lo.Map(r.Members, func(m db.GroupMemberRow, _ int) groupMemberTableRow {
			return groupMemberTableRow{m}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Members (%d)", len(rows)),
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

func buildGroupDetails(g models.ExternalGroup) api.DescriptionList {
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

// groupMemberTableRow exposes db.GroupMemberRow fields as a clicky table row.
type groupMemberTableRow struct {
	db.GroupMemberRow
}

func (r groupMemberTableRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("UserName").Label("User").Build(),
		api.Column("Email").Build(),
		api.Column("UserType").Label("Type").Build(),
		api.Column("Status").Build(),
		api.Column("LastSignedIn").Label("Last Signed In").Build(),
	}
}

func (r groupMemberTableRow) Row() map[string]any {
	status := clicky.Text("active", "text-green-600")
	if r.MembershipDeletedAt != nil {
		status = clicky.Text("removed", "text-red-600")
	}
	lastSignedIn := clicky.Text("-", "text-gray-400")
	if r.LastSignedInAt != nil {
		lastSignedIn = api.Human(time.Since(*r.LastSignedInAt), "text-gray-600")
	}
	return map[string]any{
		"UserName":     clicky.Text(r.UserName, "font-bold"),
		"Email":        clicky.Text(r.Email, "text-gray-600"),
		"UserType":     clicky.Text(r.UserType, "text-gray-500"),
		"Status":       status,
		"LastSignedIn": lastSignedIn,
	}
}

func completeAccessGroupIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	groups, err := listAccessGroups(accessGroupListOpts{Name: toComplete})
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
	return echoFilterLookup(opts.Name), nil
}
func (accessGroupNameFilter) Options(_ accessGroupListOpts) map[string]api.Textable { return nil }

type accessGroupTypeFilter struct{}

func (accessGroupTypeFilter) Key() string   { return "type" }
func (accessGroupTypeFilter) Label() string { return "Type" }
func (accessGroupTypeFilter) Lookup(opts *accessGroupListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Type), nil
}
func (accessGroupTypeFilter) Options(_ accessGroupListOpts) map[string]api.Textable { return nil }

func init() {
	clicky.RegisterEntity(clicky.Entity[models.ExternalGroup, accessGroupListOpts, any]{
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
