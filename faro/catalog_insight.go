package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	clickyapi "github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
)

var (
	insightSearchAgent string
	insightSearchLimit int
)

type catalogInsightSearchHit struct {
	ID            string                `json:"id"`
	Agent         string                `json:"agent,omitempty"`
	Name          string                `json:"name,omitempty"`
	Namespace     string                `json:"namespace,omitempty"`
	InsightType   string                `json:"insight_type,omitempty"`
	Status        string                `json:"status,omitempty"`
	Severity      *string               `json:"severity,omitempty"`
	Summary       string                `json:"summary,omitempty"`
	Config        *catalogInsightConfig `json:"config,omitempty"`
	IssueIDs      []string              `json:"issue_ids,omitempty"`
	FirstObserved *time.Time            `json:"first_observed,omitempty"`
	LastObserved  *time.Time            `json:"last_observed,omitempty"`
}

type catalogInsightConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type catalogInsightSearchResult struct {
	Items        []catalogInsightSearchHit
	Limited      bool
	TotalAtLeast int
}

type catalogInsightCompactRow struct {
	catalogInsightSearchHit
}

func (r catalogInsightCompactRow) Columns() []clickyapi.ColumnDef {
	return []clickyapi.ColumnDef{
		clickyapi.Column("ID").Build(),
		clickyapi.Column("Name").Build(),
		clickyapi.Column("Summary").Build(),
		clickyapi.Column("InsightType").Label("Insight Type").Build(),
		clickyapi.Column("Status").Build(),
		clickyapi.Column("Severity").Build(),
		clickyapi.Column("LastObserved").Label("Last Observed").Build(),
	}
}

func (r catalogInsightCompactRow) Row() map[string]any {
	severity := ""
	if r.Severity != nil {
		severity = *r.Severity
	}

	return map[string]any{
		"ID":           r.ID,
		"Name":         r.Name,
		"Summary":      r.Summary,
		"InsightType":  r.InsightType,
		"Status":       r.Status,
		"Severity":     severity,
		"LastObserved": r.LastObserved,
	}
}

func (r catalogInsightSearchHit) Columns() []clickyapi.ColumnDef {
	return []clickyapi.ColumnDef{
		clickyapi.Column("ID").Build(),
		clickyapi.Column("ConfigID").Label("Config ID").Build(),
		clickyapi.Column("ConfigName").Label("Config Name").Build(),
		clickyapi.Column("ConfigType").Label("Config Type").Build(),
		clickyapi.Column("Summary").Build(),
		clickyapi.Column("IssueIDs").Label("Issue IDs").Build(),
		clickyapi.Column("Analyzer").Build(),
		clickyapi.Column("InsightType").Label("Insight Type").Build(),
		clickyapi.Column("Status").Build(),
		clickyapi.Column("Severity").Build(),
		clickyapi.Column("FirstObserved").Label("First Observed").Build(),
		clickyapi.Column("LastObserved").Label("Last Observed").Build(),
	}
}

func (r catalogInsightSearchHit) Row() map[string]any {
	var configID, configName, configType string
	if r.Config != nil {
		configID = r.Config.ID
		configName = r.Config.Name
		configType = r.Config.Type
	}

	severity := ""
	if r.Severity != nil {
		severity = *r.Severity
	}

	return map[string]any{
		"ID":            r.ID,
		"ConfigID":      configID,
		"ConfigName":    configName,
		"ConfigType":    configType,
		"Summary":       r.Summary,
		"IssueIDs":      strings.Join(r.IssueIDs, ", "),
		"Analyzer":      r.Name,
		"InsightType":   r.InsightType,
		"Status":        r.Status,
		"Severity":      severity,
		"FirstObserved": r.FirstObserved,
		"LastObserved":  r.LastObserved,
	}
}

var CatalogInsight = &cobra.Command{
	Use:     "insights [QUERY]",
	Aliases: []string{"insight"},
	Short:   "Search and inspect catalog insights",
	Args:    cobra.ArbitraryArgs,
	RunE:    runCatalogInsightSearch,
}

var CatalogInsightSearch = &cobra.Command{
	Use:   "search [QUERY]",
	Short: "Search catalog insights using the PEG search grammar",
	Long: `Search catalog insights using the PEG search grammar used by the web UI.

Examples:
  catalog insights search severity=critical
  catalog insights search "status=open type=security"
  catalog insights search "analyzer=no-public-ip source=aws" --limit 50
	  catalog insights search "config_type=GitHub::Repository severity=critical" --limit 5
	  catalog insights search "config_id=203c4012-d12b-5c6a-a1e7-2e990f6a8f0e"`,
	Args: cobra.ArbitraryArgs,
	RunE: runCatalogInsightSearch,
}

func runCatalogInsightSearch(cmd *cobra.Command, args []string) error {
	result, err := remoteSearchInsights(catalogInsightSearchQuery(args), insightSearchAgent, insightSearchLimit)
	if err != nil {
		return err
	}
	printCatalogInsightLimitWarning(cmd, result)
	clicky.MustPrint(catalogInsightSearchOutput(result.Items, clicky.Flags.FormatOptions), clicky.Flags.FormatOptions)
	return nil
}

func catalogInsightSearchOutput(items []catalogInsightSearchHit, opts clicky.FormatOptions) any {
	format := opts.ResolveFormat()
	if format != "pretty" && format != "table" {
		return items
	}

	rows := make([]catalogInsightCompactRow, len(items))
	for i, item := range items {
		rows[i] = catalogInsightCompactRow{catalogInsightSearchHit: item}
	}
	return rows
}

func catalogInsightSearchQuery(args []string) string {
	if searchQuery := strings.Join(args, " "); searchQuery != "" {
		return searchQuery
	}
	return "status=open"
}

func printCatalogInsightLimitWarning(cmd *cobra.Command, result *catalogInsightSearchResult) {
	if result.Limited {
		fmt.Fprintf(cmd.ErrOrStderr(), "showing %d of at least %d total insights; increase --limit to return more.\n", len(result.Items), result.TotalAtLeast)
	}
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

func remoteSearchInsights(searchQuery, agent string, limit int) (*catalogInsightSearchResult, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}
	requestLimit := limit
	if requestLimit < query.MaxSearchResourcesLimit {
		requestLimit++
	}

	resp, err := client.SearchCatalog(context.Background(), query.SearchResourcesRequest{
		Limit:      requestLimit,
		Timestamps: true,
		ConfigAnalysis: []types.ResourceSelector{{
			Search: searchQuery,
			Agent:  agent,
		}},
	})
	if err != nil {
		return nil, err
	}

	totalAtLeast := len(resp.ConfigAnalysis)
	limited := len(resp.ConfigAnalysis) > limit
	if limit >= query.MaxSearchResourcesLimit && len(resp.ConfigAnalysis) == limit {
		limited = true
	}
	if len(resp.ConfigAnalysis) > limit {
		resp.ConfigAnalysis = resp.ConfigAnalysis[:limit]
	}

	ids := make([]string, len(resp.ConfigAnalysis))
	for i, item := range resp.ConfigAnalysis {
		ids[i] = item.ID
	}
	details, err := client.GetCatalogInsights(context.Background(), ids)
	if err != nil {
		return nil, err
	}
	detailsByID := make(map[string]sdk.CatalogInsightDetail, len(details))
	for _, detail := range details {
		detailsByID[detail.ID.String()] = detail
	}

	out := make([]catalogInsightSearchHit, 0, len(resp.ConfigAnalysis))
	for _, s := range resp.ConfigAnalysis {
		hit := catalogInsightSearchHit{
			ID:            s.ID,
			Agent:         s.Agent,
			Name:          s.Name,
			Namespace:     s.Namespace,
			InsightType:   s.Type,
			Status:        s.Status,
			Severity:      s.Severity,
			FirstObserved: s.CreatedAt,
			LastObserved:  s.UpdatedAt,
		}
		if detail, ok := detailsByID[s.ID]; ok {
			hit.Summary = detail.Summary
			if detail.Config != nil {
				hit.Config = &catalogInsightConfig{
					ID:   detail.Config.ID,
					Name: detail.Config.Name,
					Type: detail.Config.Type,
				}
			}
			hit.IssueIDs = catalogInsightIssueIDs(detail)
		}
		out = append(out, hit)
	}
	return &catalogInsightSearchResult{Items: out, Limited: limited, TotalAtLeast: totalAtLeast}, nil
}

func catalogInsightIssueIDs(detail sdk.CatalogInsightDetail) []string {
	issueIDs := make(map[string]struct{})
	for _, evidence := range detail.Evidences {
		if evidence.Hypothesis == nil || evidence.Hypothesis.Incident == nil {
			continue
		}
		if id := evidence.Hypothesis.Incident.IncidentID; id != "" {
			issueIDs[id] = struct{}{}
		}
	}

	result := make([]string, 0, len(issueIDs))
	for id := range issueIDs {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func remoteGetInsight(id string) (any, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}
	return client.GetCatalogInsight(context.Background(), id)
}

func init() {
	CatalogInsight.PersistentFlags().StringVar(&insightSearchAgent, "agent", "all", "Filter by agent id or name ('all' for every agent)")
	CatalogInsight.PersistentFlags().IntVar(&insightSearchLimit, "limit", 100, "Maximum number of results")
	CatalogInsight.AddCommand(CatalogInsightSearch, CatalogInsightGet)
	clicky.RegisterSubCommand("catalog", CatalogInsight)
}
