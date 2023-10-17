package cmd

import (
	"context"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/rules"
	"github.com/spf13/cobra"
)

var Run = &cobra.Command{
	Use: "run",
}

var incidentRules = &cobra.Command{
	Use:    "rules",
	PreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := api.ContextWrapFunc(context.Background())
		jr := job.JobRuntime{
			Context: ctx,
		}
		if err := rules.Run(jr); err != nil {
			logger.Fatalf("Failed to run rules: %v", err)
		}
	},
}

func init() {
	Run.AddCommand(incidentRules)
}
