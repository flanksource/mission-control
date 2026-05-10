package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/report/catalog"
)

var (
	accessMatrixExpandGroups bool
	accessMatrixRecursive    bool
)

// AccessMatrix renders the access matrix for configs matching a query. It is
// the terminal-friendly sibling of `rbac export --view matrix`, returning
// plain RBACAccessRow data that the Pretty() renderer groups by config.
var AccessMatrix = &cobra.Command{
	Use:   "matrix [QUERY...]",
	Short: "Show the user × config access matrix for a query",
	Long: `Renders every (user, config, role) triple for configs matching the query,
using the same selector syntax as 'rbac export' and 'catalog query'.

With --expand-groups, group-granted rows are expanded into one row per active
member so the matrix reflects effective access rather than the raw grant.

Examples:
  access matrix
  access matrix type=Kubernetes::Namespace name=default
  access matrix type=Azure::EnterpriseApplication --expand-groups
  access matrix nginx --recursive`,
	Args:             cobra.ArbitraryArgs,
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			return err
		}
		defer stop()

		var selectors []types.ResourceSelector
		if len(args) > 0 {
			selectors = []types.ResourceSelector{parseAccessQuery(args)}
		}

		rows, err := db.GetRBACAccess(ctx, selectors, accessMatrixRecursive)
		if err != nil {
			return err
		}

		if accessMatrixExpandGroups && len(rows) > 0 {
			configIDs := configIDsFromRows(rows)
			members, err := db.GetGroupMembersForConfigs(ctx, configIDs)
			if err != nil {
				return err
			}
			rows = catalog.ExpandGroupAccess(rows, members)
		}

		clicky.MustPrint(AccessMatrixResult{Rows: rows, Expanded: accessMatrixExpandGroups}, clicky.Flags.FormatOptions)
		return nil
	},
}

// AccessMatrixResult is the top-level printable value for `access matrix`.
// Rows carry both direct and group-mediated access entries; Pretty() groups
// them by config and surfaces whether a row came from a group via the
// Source column.
type AccessMatrixResult struct {
	Rows     []db.RBACAccessRow `json:"rows"`
	Expanded bool               `json:"expanded"`
}

func (r AccessMatrixResult) Pretty() api.Text {
	if len(r.Rows) == 0 {
		return clicky.Text("No access entries found.", "text-gray-500")
	}

	byConfig := make(map[uuid.UUID][]db.RBACAccessRow)
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
		configRows := byConfig[cid]
		rows := lo.Map(configRows, func(row db.RBACAccessRow, _ int) accessMatrixRow {
			return accessMatrixRow{row}
		})
		label := fmt.Sprintf("%s (%s) — %d", configRows[0].ConfigName, configRows[0].ConfigType, len(configRows))
		t = t.NewLine().Append(clicky.Collapsed(label, api.NewTableFrom(rows)))
	}
	return t
}

// accessMatrixRow wraps db.RBACAccessRow so we can expose custom columns
// without polluting the db layer.
type accessMatrixRow struct {
	db.RBACAccessRow
}

func (r accessMatrixRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("User").Build(),
		api.Column("Email").Build(),
		api.Column("Role").Build(),
		api.Column("Source").Build(),
		api.Column("UserType").Label("Type").Build(),
		api.Column("LastSignedIn").Label("Last Signed In").Build(),
	}
}

func (r accessMatrixRow) Row() map[string]any {
	source := clicky.Text("direct", "text-gray-500")
	if r.GroupName != nil && *r.GroupName != "" {
		source = clicky.Text("group:"+*r.GroupName, "text-purple-600")
	}
	lastSignedIn := clicky.Text("-", "text-gray-400")
	if r.LastSignedInAt != nil {
		lastSignedIn = api.Human(time.Since(*r.LastSignedInAt), "text-gray-600")
	}
	return map[string]any{
		"User":         clicky.Text(r.UserName, "font-bold"),
		"Email":        clicky.Text(r.Email, "text-gray-600"),
		"Role":         clicky.Text(r.Role),
		"Source":       source,
		"UserType":     clicky.Text(r.UserType, "text-gray-500"),
		"LastSignedIn": lastSignedIn,
	}
}

func configIDsFromRows(rows []db.RBACAccessRow) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(rows))
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if _, ok := seen[r.ConfigID]; ok {
			continue
		}
		seen[r.ConfigID] = struct{}{}
		out = append(out, r.ConfigID)
	}
	return out
}

func init() {
	AccessMatrix.Flags().BoolVar(&accessMatrixExpandGroups, "expand-groups", false, "Synthesise one row per active group member for group-granted access")
	AccessMatrix.Flags().BoolVar(&accessMatrixRecursive, "recursive", false, "Include children of every matched config")
	clicky.BindAllFlags(AccessMatrix.PersistentFlags(), "format")
	clicky.RegisterSubCommand("access", AccessMatrix)
}
