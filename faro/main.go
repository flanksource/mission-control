package main

import (
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// faro is a slimmed-down Mission Control client. It exposes only the surfaces
// that operate against a remote Mission Control server using the credentials
// obtained through the OIDC login flow.
func silenceUsage(cmd *cobra.Command) {
	cmd.SilenceUsage = true
	for _, child := range cmd.Commands() {
		silenceUsage(child)
	}
}

func main() {
	if len(commit) > 8 {
		version = fmt.Sprintf("%v, commit %v, built at %v", version, commit[0:8], date)
	}

	api.BuildVersion = version
	api.BuildCommit = commit

	root := &cobra.Command{
		Use:          "faro",
		Short:        "Slim Mission Control client",
		SilenceUsage: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version of faro",
		Args:  cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	})
	root.SetUsageTemplate(root.UsageTemplate() + fmt.Sprintf("\nversion: %s\n ", version))

	logger.BindFlags(root.PersistentFlags())
	clientcmd.RegisterClientCommands(root)

	// clicky.GenerateCLI materializes the registered remote "catalog" entity
	// (see catalog.go) into `catalog list` / `catalog get` commands.
	clicky.GenerateCLI(root)
	if c, _, err := root.Find([]string{"catalog"}); err == nil && c != nil {
		clicky.BindAllFlags(c.PersistentFlags(), "format")
	}
	silenceUsage(root)

	harFlush := func() error { return nil }
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		logger.UseCobraFlags(cmd.Flags())
		logger.UseSlog()
		harFlush = clientcmd.StartHAR()
		return nil
	}

	err := root.Execute()
	if flushErr := harFlush(); flushErr != nil {
		fmt.Fprintln(os.Stderr, flushErr)
	}
	if err != nil {
		os.Exit(1)
	}
}
