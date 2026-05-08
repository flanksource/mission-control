package main

import (
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/api"
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

	api.BuildVersion = version
	api.BuildCommit = commit

	cmd.Root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version of incident-commander",
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	})
	cmd.Root.SetUsageTemplate(cmd.Root.UsageTemplate() + fmt.Sprintf("\nversion: %s\n ", version))

	clicky.GenerateCLI(cmd.Root)
	if catalogCmd, _, err := cmd.Root.Find([]string{"catalog"}); err == nil && catalogCmd != nil {
		clicky.BindAllFlags(catalogCmd.PersistentFlags(), "format")
	}

	if err := cmd.Root.Execute(); err != nil {
		os.Exit(1)
	}
}
