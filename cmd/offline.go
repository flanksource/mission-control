package cmd

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/postgrest"
	"github.com/spf13/cobra"
)

var GoOffline = &cobra.Command{
	Use:  "go-offline",
	Long: "Download all dependencies so that incident-commander can work without an internet connection",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
	},
	Run: func(cmd *cobra.Command, args []string) {
		if err := postgrest.GoOffline(); err != nil {
			logger.Fatalf("Failed to go offline: %+v", err)
		}
	},
}
