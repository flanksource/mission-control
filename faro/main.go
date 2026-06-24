package main

import (
	gocontext "context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

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

func refreshCacheCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "refresh-cache",
		Short:        "Refresh cached metadata for the current Mission Control context",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			result, err := clientcmd.RebuildCurrentContextCache(ctx)
			if err != nil {
				return err
			}
			if result == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No Mission Control server context configured")
				return nil
			}

			if err := clientcmd.RegisterContextCachedPluginCommands(cmd.Root()); err != nil {
				return err
			}
			if err := clientcmd.RegisterContextCachedPlaybookCommands(cmd.Root()); err != nil {
				return err
			}
			sort.Strings(result.Plugins)
			sort.Strings(result.Playbooks)
			plugins := strings.Join(result.Plugins, ", ")
			if plugins == "" {
				plugins = "none"
			}
			playbooks := strings.Join(result.Playbooks, ", ")
			if playbooks == "" {
				playbooks = "none"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Refreshed cache for context %q\nCache: %s\nPlugins: %s\nPlaybooks: %s\n", result.ContextName, result.CacheDir, plugins, playbooks)
			return nil
		},
	}
}

func main() {
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 30*time.Second)
	defer cancel()

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
	root.AddCommand(refreshCacheCmd())

	refreshErr, registerErr := clientcmd.SetupContextCachedPluginCommands(ctx, root, os.Args[1:])
	if refreshErr != nil {
		log.Printf("failed to ensure context cache is upto date: %v\n", refreshErr)
	}
	if registerErr != nil {
		fmt.Fprintln(os.Stderr, registerErr)
	}

	// clicky.GenerateCLI materializes the registered remote "catalog" entity
	// (see catalog.go) into `catalog list` / `catalog get` commands.
	clicky.GenerateCLI(root)
	if c, _, err := root.Find([]string{"catalog"}); err == nil && c != nil {
		clicky.BindAllFlags(c.PersistentFlags(), "format")
	}
	clientcmd.FinalizeCommandGroups(root)
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
