package cmd

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/worker"
	"github.com/spf13/cobra"
)

var Worker = &cobra.Command{
	Use: "worker",
	Run: func(cmd *cobra.Command, args []string) {
		if err := db.Init(db.ConnectionString); err != nil {
			logger.Errorf("Failed to initialize the db: %v", err)
		}

		if err := worker.Init(); err != nil {
			logger.Errorf("Failed to initialize the worker: %v", err)
		}
	},
}
