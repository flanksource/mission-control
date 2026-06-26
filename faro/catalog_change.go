package main

import (
	"context"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/spf13/cobra"
)

var changeSearchLimit int

type catalogChangeSearchHit struct {
	ID         string     `json:"id"`
	Agent      string     `json:"agent,omitempty"`
	Name       string     `json:"name,omitempty"`
	Namespace  string     `json:"namespace,omitempty"`
	ChangeType string     `json:"change_type,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
}

var CatalogChange = &cobra.Command{
	Use:     "change",
	Aliases: []string{"changes"},
	Short:   "Search and inspect catalog changes",
}

var CatalogChangeSearch = &cobra.Command{
	Use:   "search <QUERY>",
	Short: "Search catalog changes using the PEG search grammar",
	Long: `Search catalog changes using the PEG search grammar used by the web UI.

Examples:
  catalog change search change_type=diff
  catalog change search "change_type=diff type=deployment"
  catalog change search "severity=critical source=kubernetes" --limit 50`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		results, err := remoteSearchChanges(strings.Join(args, " "), changeSearchLimit)
		if err != nil {
			return err
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		return nil
	},
}

var CatalogChangeGet = &cobra.Command{
	Use:   "get <id>",
	Short: "Get full details for a catalog change",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		change, err := remoteGetChange(args[0])
		if err != nil {
			return err
		}
		clicky.MustPrint(change, clicky.Flags.FormatOptions)
		return nil
	},
}

func remoteSearchChanges(searchQuery string, limit int) ([]catalogChangeSearchHit, error) {
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
		ConfigChanges: []types.ResourceSelector{{
			Search: searchQuery,
		}},
	})
	if err != nil {
		return nil, err
	}

	out := make([]catalogChangeSearchHit, 0, len(resp.ConfigChanges))
	for _, s := range resp.ConfigChanges {
		out = append(out, catalogChangeSearchHit{
			ID:         s.ID,
			Agent:      s.Agent,
			Name:       s.Name,
			Namespace:  s.Namespace,
			ChangeType: s.Type,
			CreatedAt:  s.CreatedAt,
		})
	}
	return out, nil
}

func remoteGetChange(id string) (any, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}
	return client.GetCatalogChange(context.Background(), id)
}

func init() {
	CatalogChangeSearch.Flags().IntVar(&changeSearchLimit, "limit", 100, "Maximum number of results")
	CatalogChange.AddCommand(CatalogChangeSearch, CatalogChangeGet)
	clicky.RegisterSubCommand("catalog", CatalogChange)
}
