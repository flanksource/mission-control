package main

import (
	"context"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/spf13/cobra"
)

var (
	searchAgent string
	searchLimit int
)

// Search backs `catalog search <QUERY>`. Unlike `catalog list`, which composes
// individual filter flags, search takes the full PEG search grammar as a single
// query and forwards it to the remote /resources/search endpoint — the same
// surface the web UI uses (e.g. `tags.cluster=beta type=pod my-app`).
var Search = &cobra.Command{
	Use:   "search <QUERY>",
	Short: "Search catalog resources using the PEG search grammar",
	Long: `Search catalog resources using the PEG search grammar used by the web UI.

The query is matched against resource name, type, tags and labels. Multiple
clauses are space-separated and ANDed together.

Examples:
  catalog search type=Kubernetes::Pod
  catalog search tags.cluster=beta-cluster type=pod mission-control
  catalog search "name=api*" --agent all --limit 50`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		results, err := remoteSearch(strings.Join(args, " "), searchAgent, searchLimit)
		if err != nil {
			return err
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		return nil
	},
}

// remoteSearch runs the grammar search against the remote server and maps the
// lightweight search hits to models.ConfigItem so they render like `catalog list`.
func remoteSearch(searchQuery, agent string, limit int) ([]models.ConfigItem, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	resp, err := client.SearchCatalog(context.Background(), query.SearchResourcesRequest{
		Limit:      limit,
		Timestamps: true,
		Configs: []types.ResourceSelector{{
			Search: searchQuery,
			Agent:  agent,
		}},
	})
	if err != nil {
		return nil, err
	}

	out := make([]models.ConfigItem, 0, len(resp.Configs))
	for _, s := range resp.Configs {
		out = append(out, selectedResourceToConfigItem(s))
	}
	return out, nil
}

func init() {
	Search.Flags().StringVar(&searchAgent, "agent", "all", "Filter by agent id or name ('all' for every agent)")
	Search.Flags().IntVar(&searchLimit, "limit", 100, "Maximum number of results")
	clicky.RegisterSubCommand("catalog", Search)
}
