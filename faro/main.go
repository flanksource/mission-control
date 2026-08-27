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

	build := currentBuildInfo()
	api.BuildVersion = build.String()
	api.BuildCommit = build.Commit

	root := &cobra.Command{
		Use:          "faro",
		Short:        "Slim Mission Control client",
		Long: `Faro is a slim client for inspecting and operating a remote Mission Control server.

When troubleshooting a failed or unhealthy catalog config, start with its
change history. Include related configs to correlate upstream and downstream
changes around the failure, then inspect the relevant change in full:

  faro catalog change search --config <config-id> --related all --soft
  faro catalog change get <change-id>

Use --soft to include cross-domain relationships, such as between a Git
repository, Kubernetes deployment, and Postgres database, so commits, merges,
migrations, deployments, and failures can be compared in one timeline. Without
--soft, relationship traversal follows only direct parent-child relationships.

Change search results are ordered newest first. Use "faro catalog search" to
find a config ID and "faro catalog get" to inspect its current state.`,
		SilenceUsage: true,
	}

	root.AddCommand(versionCmd())
	root.SetUsageTemplate(root.UsageTemplate() + fmt.Sprintf("\nversion: %s\n ", build))

	logger.BindFlags(root.PersistentFlags())
	clientcmd.RegisterClientCommands(root)
	root.AddCommand(Catalog, refreshCacheCmd())

	refreshErr, registerErr := clientcmd.SetupContextCachedPluginCommands(ctx, root, os.Args[1:])
	if refreshErr != nil {
		log.Printf("failed to ensure context cache is upto date: %v\n", refreshErr)
	}
	if registerErr != nil {
		fmt.Fprintln(os.Stderr, registerErr)
	}

	// clicky.GenerateCLI materializes the registered remote entities — "catalog"
	// (see catalog.go) and the "access" users/groups/roles (see access_*.go) —
	// into their `list` / `get` commands.
	clicky.GenerateCLI(root)
	if c, _, err := root.Find([]string{"catalog"}); err == nil && c != nil {
		documentCatalogCommand(c)
		clicky.BindAllFlags(c.PersistentFlags(), "format")
	}
	if c, _, err := root.Find([]string{"access"}); err == nil && c != nil {
		documentAccessCommand(c)
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
