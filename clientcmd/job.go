package clientcmd

import (
	"github.com/flanksource/clicky"
	sdk "github.com/flanksource/incident-commander/sdk/client"
	"github.com/spf13/cobra"
)

var (
	jobListName         string
	jobListStatus       []string
	jobListResourceType string
	jobListResourceID   string
	jobListAgentID      string
	jobListHasErrors    bool
	jobListSince        string
	jobListBefore       string
	jobListLimit        int
)

// Job is the parent command for persisted job execution history.
var Job = &cobra.Command{
	Use:   "job",
	Short: "Inspect job execution history",
	Long: `List and inspect persisted job runs from the configured Mission Control server.

Use this to debug scrapers and other background jobs: last successful run,
recent failures, and the complete details of a single execution.`,
}

var JobList = &cobra.Command{
	Use:     "list",
	Aliases: []string{"history"},
	Short:   "List persisted job execution history",
	Long: `List persisted job execution history, ordered by most recent run first.

Returns compact diagnostics. Use "job get" for complete details and errors.

Examples:
  faro job list --name scraper --status FAILED --since now-24h
  faro job list --resource-id <scraper-id> --has-errors
  faro job list --resource-type config_scraper --limit 50`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, client, err := playbookAPIClient(cmd)
		if err != nil {
			return err
		}

		opts := sdk.JobRunListOptions{
			Name:         jobListName,
			Statuses:     jobListStatus,
			ResourceType: jobListResourceType,
			ResourceID:   jobListResourceID,
			AgentID:      jobListAgentID,
			Since:        jobListSince,
			Before:       jobListBefore,
			Limit:        jobListLimit,
		}
		if cmd.Flags().Changed("has-errors") {
			opts.HasErrors = &jobListHasErrors
		}

		runs, err := client.ListJobRuns(cmd.Context(), opts)
		if err != nil {
			return err
		}
		return printClicky(cmd.OutOrStdout(), jobRuns(runs), "pretty")
	},
}

var JobGet = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a persisted job execution",
	Long: `Get the complete persisted record for a job execution, including its details and errors.

Use "job list" to find a run ID first.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, client, err := playbookAPIClient(cmd)
		if err != nil {
			return err
		}
		run, err := client.GetJobRun(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printClicky(cmd.OutOrStdout(), jobRun(*run), "pretty")
	},
}

func init() {
	JobList.Flags().StringVar(&jobListName, "name", "", "Case-insensitive partial match against the job name")
	JobList.Flags().StringSliceVar(&jobListStatus, "status", nil, "Filter by status: RUNNING, SUCCESS, WARNING, FAILED, STALE, SKIPPED")
	JobList.Flags().StringVar(&jobListResourceType, "resource-type", "", "Exact resource type filter")
	JobList.Flags().StringVar(&jobListResourceID, "resource-id", "", "Exact resource ID filter")
	JobList.Flags().StringVar(&jobListAgentID, "agent-id", "", "Exact agent ID filter")
	JobList.Flags().BoolVar(&jobListHasErrors, "has-errors", false, "Filter for runs with (true) or without (false) errors")
	JobList.Flags().StringVar(&jobListSince, "since", "", "Inclusive start time in date-math or RFC3339 format, e.g. now-24h")
	JobList.Flags().StringVar(&jobListBefore, "before", "", "Inclusive end time in date-math or RFC3339 format, e.g. now-1h")
	JobList.Flags().IntVar(&jobListLimit, "limit", 20, "Maximum runs to return (maximum: 100)")
	clicky.BindAllFlags(JobList.Flags(), "format")
	clicky.BindAllFlags(JobGet.Flags(), "format")

	Job.AddCommand(JobList, JobGet)
}
