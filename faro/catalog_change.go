package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	changeSearchConfig  string
	changeSearchDepth   int
	changeSearchLimit   int
	changeSearchRelated string
	changeSearchSoft    bool
)

type catalogChangeSearchOptions struct {
	ConfigID   string
	Depth      int
	DepthSet   bool
	Limit      int
	Related    clientapi.ChangeRelationDirection
	RelatedSet bool
	Soft       bool
	SoftSet    bool
}

type catalogChangeSearchHit struct {
	ID         string     `json:"id"`
	ConfigID   string     `json:"config_id,omitempty"`
	Agent      string     `json:"agent,omitempty"`
	Name       string     `json:"name,omitempty"`
	Namespace  string     `json:"namespace,omitempty"`
	ConfigType string     `json:"config_type,omitempty"`
	ChangeType string     `json:"change_type,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
}

var CatalogChange = &cobra.Command{
	Use:     "change",
	Aliases: []string{"changes"},
	Short:   "Search and inspect catalog changes",
}

var CatalogChangeSearch = &cobra.Command{
	Use:   "search [QUERY]",
	Short: "Search global or config-scoped catalog changes",
	Long: `Search catalog changes globally using the PEG search grammar, or fetch
changes for a catalog config and its related configs.

Examples:
  catalog change search change_type=diff
  catalog change search "change_type=diff type=deployment"
  catalog change search --config <config-id>
  catalog change search --config <config-id> --related downstream --soft --depth 5`,
	Args: func(cmd *cobra.Command, args []string) error {
		return validateCatalogChangeSearch(strings.Join(args, " "), catalogChangeSearchOptions{
			ConfigID:   changeSearchConfig,
			Depth:      changeSearchDepth,
			DepthSet:   cmd.Flags().Changed("depth"),
			Limit:      changeSearchLimit,
			Related:    clientapi.ChangeRelationDirection(changeSearchRelated),
			RelatedSet: cmd.Flags().Changed("related"),
			Soft:       changeSearchSoft,
			SoftSet:    cmd.Flags().Changed("soft"),
		})
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var results []catalogChangeSearchHit
		var err error
		if strings.TrimSpace(changeSearchConfig) != "" {
			results, err = remoteSearchRelatedChanges(catalogChangeSearchOptions{
				ConfigID: strings.TrimSpace(changeSearchConfig),
				Depth:    changeSearchDepth,
				Limit:    changeSearchLimit,
				Related:  clientapi.ChangeRelationDirection(changeSearchRelated),
				Soft:     changeSearchSoft,
			})
		} else {
			results, err = remoteSearchChanges(strings.Join(args, " "), changeSearchLimit)
		}
		if err != nil {
			return err
		}
		clicky.MustPrint(results, clicky.Flags.FormatOptions)
		return nil
	},
}

func validateCatalogChangeSearch(searchQuery string, opts catalogChangeSearchOptions) error {
	hasQuery := strings.TrimSpace(searchQuery) != ""
	hasConfig := strings.TrimSpace(opts.ConfigID) != ""

	if !hasConfig && (opts.RelatedSet || opts.SoftSet || opts.DepthSet) {
		return fmt.Errorf("--related, --soft, and --depth require --config")
	}
	if !hasQuery && !hasConfig {
		return fmt.Errorf("a search query or --config is required")
	}
	if hasQuery && hasConfig {
		return fmt.Errorf("search query and --config cannot be used together")
	}
	if !hasConfig {
		return nil
	}
	if _, err := uuid.Parse(opts.ConfigID); err != nil {
		return fmt.Errorf("invalid --config UUID: %w", err)
	}

	switch opts.Related {
	case clientapi.CatalogChangeRecursiveNone,
		clientapi.CatalogChangeRecursiveDownstream,
		clientapi.CatalogChangeRecursiveUpstream,
		clientapi.CatalogChangeRecursiveAll:
	default:
		return fmt.Errorf("--related must be one of none, downstream, upstream, or all")
	}
	if opts.Depth <= 0 {
		return fmt.Errorf("--depth must be greater than zero")
	}
	if opts.Related == clientapi.CatalogChangeRecursiveNone && (opts.SoftSet || opts.DepthSet) {
		return fmt.Errorf("--soft and --depth require --related to be downstream, upstream, or all")
	}
	return nil
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

	resp, err := client.SearchCatalog(context.Background(), clientapi.SearchResourcesRequest{
		Limit:      limit,
		Timestamps: true,
		ConfigChanges: []clientapi.ResourceSelector{{
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

func remoteSearchRelatedChanges(opts catalogChangeSearchOptions) ([]catalogChangeSearchHit, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}

	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	resp, err := client.SearchCatalogChanges(context.Background(), clientapi.CatalogChangesSearchRequest{
		BaseCatalogSearch: clientapi.BaseCatalogSearch{
			CatalogID: opts.ConfigID,
			Depth:     opts.Depth,
			PageSize:  opts.Limit,
			Recursive: opts.Related,
			Soft:      opts.Soft,
			SortBy:    "-created_at",
		},
	})
	if err != nil {
		return nil, err
	}

	out := make([]catalogChangeSearchHit, 0, len(resp.Changes))
	for _, change := range resp.Changes {
		out = append(out, catalogChangeSearchHit{
			ID:         change.ID,
			ConfigID:   change.ConfigID,
			Agent:      change.AgentID,
			Name:       change.ConfigName,
			Namespace:  change.Tags["namespace"],
			ConfigType: change.ConfigType,
			ChangeType: change.ChangeType,
			CreatedAt:  change.CreatedAt,
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
	CatalogChangeSearch.Flags().StringVar(&changeSearchConfig, "config", "", "Catalog config UUID to scope changes")
	CatalogChangeSearch.Flags().IntVar(&changeSearchDepth, "depth", 5, "Maximum relationship traversal depth")
	CatalogChangeSearch.Flags().IntVar(&changeSearchLimit, "limit", 100, "Maximum number of results")
	CatalogChangeSearch.Flags().StringVar(&changeSearchRelated, "related", string(clientapi.CatalogChangeRecursiveNone), "Related config direction: none, downstream, upstream, or all")
	CatalogChangeSearch.Flags().BoolVar(&changeSearchSoft, "soft", false, "Include soft relationships")
	CatalogChange.AddCommand(CatalogChangeSearch, CatalogChangeGet)
	Catalog.AddCommand(CatalogChange)
}
