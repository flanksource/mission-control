package main

import (
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

var (
	accessLogsUser  string
	accessLogsSince string
	accessLogsLimit int
)

// AccessLogs exports the sign-in records that back the catalog report's access
// log section.
var AccessLogs = &cobra.Command{
	Use:   "logs [QUERY...]",
	Short: "Export config sign-in records",
	Long: `Exports one row per recorded sign-in against a config, newest first.

QUERY uses the same PEG search grammar as 'catalog search' and narrows the
export to the matching configs. Without a query every config is included.

Examples:
  faro access logs --since 30d
  faro access logs type=Azure::EnterpriseApplication --csv
  faro access logs --user '*@example.com,!svc-*' --since 1y --limit 500`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ctx, err := accessClient()
		if err != nil {
			return err
		}

		configIDs, err := resolveConfigIDs(ctx, client, args)
		if err != nil {
			return err
		}
		userIDs, err := resolveUserIDs(ctx, client, accessLogsUser)
		if err != nil {
			return err
		}
		since, err := parseSince(accessLogsSince)
		if err != nil {
			return err
		}
		if accessLogsUser != "" && len(userIDs) == 0 {
			printAccessResult(AccessLogsResult{}, accessLogRows(nil))
			return nil
		}

		logs, total, err := client.ListAccessLogs(ctx, sdk.AccessHistoryOptions{
			ConfigIDs: configIDs,
			UserIDs:   userIDs,
			Since:     since,
			Limit:     accessLogsLimit,
		})
		if err != nil {
			return err
		}
		warnTruncated("access logs", len(logs), total)

		printAccessResult(AccessLogsResult{Rows: logs}, accessLogRows(logs))
		return nil
	},
}

// AccessLogsResult is the printable value for `access logs`.
type AccessLogsResult struct {
	Rows []sdk.AccessLog `json:"rows"`
}

func (r AccessLogsResult) Pretty() api.Text {
	if len(r.Rows) == 0 {
		return clicky.Text("No access logs found.", "text-gray-500")
	}
	return clicky.Text(fmt.Sprintf("Access logs: %d sign-ins", len(r.Rows)), "font-bold text-gray-700").
		NewLine().Append(api.NewTableFrom(accessLogRows(r.Rows)))
}

func accessLogRows(logs []sdk.AccessLog) []accessLogRow {
	return lo.Map(logs, func(l sdk.AccessLog, _ int) accessLogRow { return accessLogRow{l} })
}

func init() {
	AccessLogs.Flags().StringVar(&accessLogsUser, "user", "", "Filter id, name, email or alias with MatchItem patterns")
	AccessLogs.Flags().StringVar(&accessLogsSince, "since", "90d", "Only include sign-ins newer than this age (e.g. 24h, 30d, 1y)")
	AccessLogs.Flags().IntVar(&accessLogsLimit, "limit", 100, "Maximum number of rows (0 for no limit)")
	clicky.BindAllFlags(AccessLogs.PersistentFlags(), "format")
	clicky.RegisterSubCommand("access", AccessLogs)
}
