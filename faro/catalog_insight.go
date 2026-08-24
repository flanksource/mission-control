package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	clickyapi "github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/clientcmd/mccontext"
	sdk "github.com/flanksource/incident-commander/sdk/client"
	"github.com/spf13/cobra"
)

var (
	insightSearchAgent string
	insightSearchFull  bool
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
	Details      []sdk.CatalogInsightDetail
	Limited      bool
	TotalAtLeast int
}

func (r catalogInsightSearchHit) Columns() []clickyapi.ColumnDef {
	return []clickyapi.ColumnDef{
		clickyapi.Column("ID").Build(),
		clickyapi.Column("ConfigID").Label("Config ID").Build(),
		clickyapi.Column("ConfigName").Label("Config Name").Build(),
		clickyapi.Column("ConfigType").Label("Config Type").Build(),
		clickyapi.Column("Name").Build(),
		clickyapi.Column("Summary").Build(),
		clickyapi.Column("InsightType").Label("Insight Type").Build(),
		clickyapi.Column("Status").Build(),
		clickyapi.Column("Severity").Build(),
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
		"ID":           r.ID,
		"ConfigID":     configID,
		"ConfigName":   configName,
		"ConfigType":   configType,
		"Name":         r.Name,
		"Summary":      r.Summary,
		"InsightType":  r.InsightType,
		"Status":       r.Status,
		"Severity":     severity,
		"LastObserved": r.LastObserved,
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
	Use:     "search [QUERY]",
	Aliases: []string{"list"},
	Short:   "Search catalog insights using the PEG search grammar",
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
	clicky.MustPrint(catalogInsightSearchOutput(result, insightSearchFull), clicky.Flags.FormatOptions)
	return nil
}

func catalogInsightSearchOutput(result *catalogInsightSearchResult, full bool) any {
	if full {
		return catalogInsightDetailViews(result.Details)
	}
	return result.Items
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

// insightSearchUnlimited is the limit that asks for every matching insight. The search
// endpoint caps a single request at query.MaxSearchResourcesLimit, so anything larger —
// including "all of them" — is reached by paging rather than by a bigger request.
const insightSearchUnlimited = 0

// searchInsightPage reads one page. `sort=id` gives the scan a stable total order, which
// offset paging is meaningless without.
func searchInsightPage(client *sdk.Client, searchQuery, agent string, size, offset int) ([]clientapi.SelectedResource, error) {
	paged := fmt.Sprintf("%s sort=id offset=%d", searchQuery, offset)
	resp, err := client.SearchCatalog(context.Background(), clientapi.SearchResourcesRequest{
		Limit:      size,
		Timestamps: true,
		ConfigAnalysis: []clientapi.ResourceSelector{{
			Search: strings.TrimSpace(paged),
			Agent:  agent,
		}},
	})
	if err != nil {
		return nil, err
	}
	return resp.ConfigAnalysis, nil
}

// searchInsightIDs pages until the server returns a short page, deduplicating as it goes.
//
// The dedup is load-bearing, not defensive: the search view is built without an ORDER BY
// of its own (duty queryTableWithResourceSelectors), and offset paging over it repeats the
// row on every page boundary — measured as one duplicate per boundary against a live
// catalog. Discarding a row already seen keeps the result correct whether or not that is
// fixed upstream.
// Returns up to limit+1 rows: the extra row is what separates "exactly limit matches"
// from "limit matches and more remain", and the caller truncates it away.
func searchInsightIDs(client *sdk.Client, searchQuery, agent string, limit int) ([]clientapi.SelectedResource, bool, error) {
	want := insightSearchUnlimited
	if limit > insightSearchUnlimited {
		want = limit + 1
	}

	size := clientapi.MaxSearchResourcesLimit
	if want > insightSearchUnlimited && want < size {
		size = want
	}

	var selected []clientapi.SelectedResource
	seen := make(map[string]struct{})
	for offset := 0; ; offset += size {
		page, err := searchInsightPage(client, searchQuery, agent, size, offset)
		if err != nil {
			return nil, false, err
		}
		fresh := 0
		for _, item := range page {
			if _, duplicate := seen[item.ID]; duplicate {
				continue
			}
			seen[item.ID] = struct{}{}
			selected = append(selected, item)
			fresh++
			if want > insightSearchUnlimited && len(selected) == want {
				return selected, true, nil
			}
		}
		if len(page) < size {
			return selected, false, nil
		}
		// One duplicate per page boundary is expected; a whole page of them is not. It
		// means the server ignored `offset` and is serving the same page, so continuing
		// would loop forever rather than eventually finish.
		if fresh == 0 {
			return nil, false, fmt.Errorf(
				"insight search repeated all %d rows at offset %d; the server is ignoring offset, so paging cannot complete",
				len(page), offset)
		}
	}
}

// searchSetsPagingKey reports a search expression that sets a key paging owns. It matches
// whole tokens rather than substrings, so a value that merely contains one of them —
// `name=resort=weekly` — is not mistaken for the caller setting a sort key.
func searchSetsPagingKey(searchQuery string) (string, bool) {
	for _, field := range strings.Fields(searchQuery) {
		for _, key := range []string{"sort", "offset"} {
			if strings.HasPrefix(field, key+"=") {
				return key, true
			}
		}
	}
	return "", false
}

func remoteSearchInsights(searchQuery, agent string, limit int) (*catalogInsightSearchResult, error) {
	// A caller who hand-rolled paging must not get a second, conflicting one layered
	// underneath, so this is rejected rather than quietly overridden.
	if key, set := searchSetsPagingKey(searchQuery); set {
		return nil, fmt.Errorf("insight search %q sets %s; paging owns sort and offset, so remove it from the search expression", searchQuery, key)
	}

	client, err := mccontext.RemoteClient()
	if err != nil {
		return nil, err
	}

	selected, limited, err := searchInsightIDs(client, searchQuery, agent, limit)
	if err != nil {
		return nil, err
	}
	// Counted before the extra row is dropped, so it stays a true lower bound: exact
	// when nothing was truncated, and one past the limit when something was.
	totalAtLeast := len(selected)
	if limited {
		selected = selected[:limit]
	}
	ids := make([]string, len(selected))
	for i, item := range selected {
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

	out := make([]catalogInsightSearchHit, 0, len(selected))
	orderedDetails := make([]sdk.CatalogInsightDetail, 0, len(selected))
	for _, s := range selected {
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
			orderedDetails = append(orderedDetails, detail)
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
	return &catalogInsightSearchResult{Items: out, Details: orderedDetails, Limited: limited, TotalAtLeast: totalAtLeast}, nil
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
	client, err := mccontext.RemoteClient()
	if err != nil {
		return nil, err
	}
	detail, err := client.GetCatalogInsight(context.Background(), id)
	if err != nil {
		return nil, err
	}
	view := catalogInsightDetailViewOf(*detail)
	return &view, nil
}

func init() {
	CatalogInsight.PersistentFlags().StringVar(&insightSearchAgent, "agent", "all", "Filter by agent id or name ('all' for every agent)")
	CatalogInsight.PersistentFlags().IntVar(&insightSearchLimit, "limit", 100,
		"Maximum number of results; 0 returns every match, paging the whole catalog if needed")
	CatalogInsight.Flags().BoolVar(&insightSearchFull, "full", false, "Return full insight records")
	CatalogInsightSearch.Flags().BoolVar(&insightSearchFull, "full", false, "Return full insight records")
	CatalogInsight.AddCommand(CatalogInsightSearch, CatalogInsightGet)
	Catalog.AddCommand(CatalogInsight)
}
