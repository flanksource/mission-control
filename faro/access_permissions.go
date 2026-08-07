package main

import (
	"context"
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
)

var (
	permissionsConfigType   string
	permissionsUser         string
	permissionsRole         string
	permissionsUserType     string
	permissionsExpandGroups bool
	permissionsByUser       bool
	permissionsByConfig     bool
	permissionsLimit        int
)

// AccessPermissions exports the (config, principal, role) grants that back the
// access crosstab in the catalog report. It is the remote-only sibling of
// `mission-control access matrix`, hence the alias.
var AccessPermissions = &cobra.Command{
	Use:     "permissions [QUERY...]",
	Aliases: []string{"matrix"},
	Short:   "Export the config × principal access matrix",
	Long: `Exports one row per (config, principal, role) grant — the crosstab data behind
the catalog report's access matrix.

QUERY uses the same PEG search grammar as 'catalog search' and narrows the
export to the matching configs. Without a query every config is exported.

--expand-groups synthesises one row per active member of a granting group, so
the export reflects effective access rather than the raw grant.

Examples:
  faro access permissions --limit 20
  faro access permissions type=Azure::EnterpriseApplication --config-type 'Azure::*' --csv
  faro access permissions --role 'Owner,Reader*,!Legacy*' --expand-groups
  faro access permissions --user '*@example.com,!svc-*'
  faro access permissions --by-user
  faro access permissions type=Kubernetes::Namespace --by-config`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if permissionsByUser && permissionsByConfig {
			return fmt.Errorf("--by-user and --by-config are mutually exclusive")
		}

		client, ctx, err := accessClient()
		if err != nil {
			return err
		}

		configIDs, err := resolveConfigIDs(ctx, client, args)
		if err != nil {
			return err
		}
		userIDs, err := resolveUserIDs(ctx, client, permissionsUser)
		if err != nil {
			return err
		}

		opts := sdk.AccessGrantOptions{
			ConfigIDs:  configIDs,
			UserIDs:    userIDs,
			ConfigType: permissionsConfigType,
			Role:       permissionsRole,
			UserType:   permissionsUserType,
			Limit:      permissionsLimit,
		}
		if permissionsUser != "" && len(userIDs) == 0 {
			switch {
			case permissionsByUser:
				printAccessResult(AccessSummaryByUserResult{}, userSummaryRows(nil))
			case permissionsByConfig:
				printAccessResult(AccessSummaryByConfigResult{}, configSummaryRows(nil))
			default:
				printAccessResult(AccessPermissionsResult{Expanded: permissionsExpandGroups}, principalGrantRows(nil))
			}
			return nil
		}

		switch {
		case permissionsByUser:
			return printAccessByUser(ctx, client, opts)
		case permissionsByConfig:
			return printAccessByConfig(ctx, client, opts)
		}

		grants, total, err := client.ListAccessGrants(ctx, opts)
		if err != nil {
			return err
		}
		warnTruncated("access entries", len(grants), total)

		if permissionsExpandGroups {
			if grants, err = expandGroups(ctx, client, grants); err != nil {
				return err
			}
		}

		printAccessResult(AccessPermissionsResult{Rows: grants, Expanded: permissionsExpandGroups}, principalGrantRows(grants))
		return nil
	},
}

// expandGroups replaces group-held grants with one row per active member.
func expandGroups(ctx context.Context, client *sdk.Client, grants []sdk.AccessGrant) ([]sdk.AccessGrant, error) {
	groupIDs := make([]string, 0)
	for _, g := range grants {
		if g.ExternalGroupID != nil {
			groupIDs = append(groupIDs, g.ExternalGroupID.String())
		}
	}
	if len(groupIDs) == 0 {
		return grants, nil
	}

	members, err := client.GetGroupMembers(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	return sdk.ExpandGroupAccess(grants, members), nil
}

// printAccessByUser reads the server rollup when the filters allow it, and
// otherwise aggregates the filtered grant rows client-side.
func printAccessByUser(ctx context.Context, client *sdk.Client, opts sdk.AccessGrantOptions) error {
	if opts.CanUseUserRollup() {
		rows, total, err := client.ListAccessSummaryByUser(ctx, opts.Limit)
		if err != nil {
			return err
		}
		warnTruncated("users", len(rows), total)
		printAccessResult(AccessSummaryByUserResult{Rows: rows}, userSummaryRows(rows))
		return nil
	}

	grants, total, err := client.ListAccessGrants(ctx, opts)
	if err != nil {
		return err
	}
	warnTruncated("access entries", len(grants), total)
	rows := rollupByUser(grants)
	printAccessResult(AccessSummaryByUserResult{Rows: rows, Derived: true}, userSummaryRows(rows))
	return nil
}

func printAccessByConfig(ctx context.Context, client *sdk.Client, opts sdk.AccessGrantOptions) error {
	if opts.CanUseConfigRollup() {
		rows, total, err := client.ListAccessSummaryByConfig(ctx, opts)
		if err != nil {
			return err
		}
		warnTruncated("configs", len(rows), total)
		printAccessResult(AccessSummaryByConfigResult{Rows: rows}, configSummaryRows(rows))
		return nil
	}

	grants, total, err := client.ListAccessGrants(ctx, opts)
	if err != nil {
		return err
	}
	warnTruncated("access entries", len(grants), total)
	rows := rollupByConfig(grants)
	printAccessResult(AccessSummaryByConfigResult{Rows: rows, Derived: true}, configSummaryRows(rows))
	return nil
}

func init() {
	AccessPermissions.Flags().StringVar(&permissionsConfigType, "config-type", "", "Filter config type with MatchItem patterns")
	AccessPermissions.Flags().StringVar(&permissionsUser, "user", "", "Filter id, name, email or alias with MatchItem patterns")
	AccessPermissions.Flags().StringVar(&permissionsRole, "role", "", "Filter role name with MatchItem patterns")
	AccessPermissions.Flags().StringVar(&permissionsUserType, "user-type", "", "Filter user type with MatchItem patterns")
	AccessPermissions.Flags().BoolVar(&permissionsExpandGroups, "expand-groups", false, "Synthesise one row per active group member for group-granted access")
	AccessPermissions.Flags().BoolVar(&permissionsByUser, "by-user", false, "Roll up to one row per user instead of per grant")
	AccessPermissions.Flags().BoolVar(&permissionsByConfig, "by-config", false, "Roll up to one row per config instead of per grant")
	AccessPermissions.Flags().IntVar(&permissionsLimit, "limit", 0, "Maximum number of rows (0 for no limit)")
	clicky.BindAllFlags(AccessPermissions.PersistentFlags(), "format")
	clicky.RegisterSubCommand("access", AccessPermissions)
}
