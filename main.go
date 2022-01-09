package main

import (
	"fmt"
	"os"

	"github.com/flanksource/incident-commander/cmd"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(commit) > 8 {
		version = fmt.Sprintf("%v, commit %v, built at %v", version, commit[0:8], date)
	}

	cmd.Root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version of incident-commander",
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	})
	cmd.Root.SetUsageTemplate(cmd.Root.UsageTemplate() + fmt.Sprintf("\nversion: %s\n ", version))

	if err := cmd.Root.Execute(); err != nil {
		os.Exit(1)
	}
}
