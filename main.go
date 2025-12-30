package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

	cmd.Root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version of incident-commander",
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	})
	cmd.Root.SetUsageTemplate(cmd.Root.UsageTemplate() + fmt.Sprintf("\nversion: %s\n ", version))

	cmd.Root.SilenceErrors = true
	cmd.Root.SilenceUsage = true

	if err := cmd.Root.Execute(); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
