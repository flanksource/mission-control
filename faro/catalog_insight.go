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

var (
	insightSearchAgent string
	insightSearchLimit int
)

type catalogInsightSearchHit struct {
	ID          string     `json:"id"`
	Agent       string     `json:"agent,omitempty"`
	Name        string     `json:"name,omitempty"`
	Namespace   string     `json:"namespace,omitempty"`
	InsightType string     `json:"insight_type,omitempty"`
	Status      string     `json:"status,omitempty"`
	Severity    *string    `json:"severity,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

var CatalogInsight = &cobra.Command{
	Use:     "insights",
	Aliases: []string{"insight"},
	Short:   "Search and inspect catalog insights",
}

var CatalogInsightSearch = &cobra.Command{
	Use:   "search <QUERY>",
	Short: "Search catalog insights using the PEG search grammar",
	Long: `Search catalog insights using the PEG search grammar used by the web UI.

Examples:
  catalog insights search severity=critical
  catalog insights search "status=open type=security"
  catalog insights search "analyzer=no-public-ip source=aws" --limit 50
  catalog insights search "config_type=GitHub::Repository severity=critical" --limit 5
  catalog insights search "config_id=203c4012-d12b-5c6a-a1e7-2e990f6a8f0e"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		results, err := remoteSearchInsights(strings.Join(args, " "), insightSearchAgent, insightSearchLimit)
		if err != nil {
			return err
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		return nil
	},
}

var CatalogInsightGet = &cobra.Command{
	Use:   "get <id>",
	Short: "Get full details for a catalog insight",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		insight, err := remoteGetInsight(args[0])
		if err != nil {
			return err
		}
		clicky.MustPrint(insight, clicky.Flags.FormatOptions)
		return nil
	},
}

func remoteSearchInsights(searchQuery, agent string, limit int) ([]catalogInsightSearchHit, error) {
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
		ConfigAnalysis: []types.ResourceSelector{{
			Search: searchQuery,
			Agent:  agent,
		}},
	})
	if err != nil {
		return nil, err
	}

	out := make([]catalogInsightSearchHit, 0, len(resp.ConfigAnalysis))
	for _, s := range resp.ConfigAnalysis {
		out = append(out, catalogInsightSearchHit{
			ID:          s.ID,
			Agent:       s.Agent,
			Name:        s.Name,
			Namespace:   s.Namespace,
			InsightType: s.Type,
			Status:      s.Status,
			Severity:    s.Severity,
			CreatedAt:   s.CreatedAt,
			UpdatedAt:   s.UpdatedAt,
		})
	}
	return out, nil
}

func remoteGetInsight(id string) (any, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}
	return client.GetCatalogInsight(context.Background(), id)
}

func init() {
	CatalogInsightSearch.Flags().StringVar(&insightSearchAgent, "agent", "all", "Filter by agent id or name ('all' for every agent)")
	CatalogInsightSearch.Flags().IntVar(&insightSearchLimit, "limit", 100, "Maximum number of results")
	CatalogInsight.AddCommand(CatalogInsightSearch, CatalogInsightGet)
	clicky.RegisterSubCommand("catalog", CatalogInsight)
}
