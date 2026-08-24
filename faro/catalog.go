package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/clientcmd/mccontext"
	sdk "github.com/flanksource/incident-commander/sdk/client"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// catalogListOpts binds the `catalog list` filter flags via clicky's `flag:` tags.
type catalogListOpts struct {
	Query     string   `flag:"query" help:"Free-form text or catalog query expression"`
	Type      string   `flag:"type" help:"Filter by type (comma-separated, supports ! negation)"`
	Namespace string   `flag:"namespace" help:"Filter by namespace"`
	Tag       []string `flag:"tag" help:"Filter by tag as a label selector (repeatable: --tag cluster=foo)"`
	Agent     string   `flag:"agent" help:"Filter by agent id or name ('all' for every agent)" default:"all"`
	Limit     int      `flag:"limit" help:"Maximum number of results" default:"100"`
	Full      bool     `flag:"full" help:"Return complete catalog items"`
}

// catalogGetFlags binds the `catalog get` flags.
type catalogGetFlags struct {
	Relationships bool `flag:"relationships" help:"Return the config relationship tree instead of the item"`
}

var catalogListOptions catalogListOpts
var catalogGetOptions catalogGetFlags

var Catalog = &cobra.Command{
	Use:     "catalog",
	Aliases: []string{"configs"},
	Short:   "Manage catalog resources",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

var CatalogList = &cobra.Command{
	Use:   "list",
	Short: "List catalog resources",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		items, err := remoteList(catalogListOptions)
		if err != nil {
			return err
		}
		clicky.MustPrint(catalogListItems(items), clicky.Flags.FormatOptions)
		return nil
	},
}

var CatalogGet = &cobra.Command{
	Use:               "get <id>",
	Aliases:           []string{"inspect"},
	Short:             "Get a catalog resource",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeCatalogIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := remoteGet(args[0], map[string]string{
			"relationships": strconv.FormatBool(catalogGetOptions.Relationships),
		})
		if err != nil {
			return err
		}
		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		return nil
	},
}

// joinTagSelectors flattens repeated --tag values into a comma-separated label
// selector, stripping the stray brackets clicky's []string round-trip can add.
func joinTagSelectors(tags []string) string {
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "[")
		t = strings.TrimSuffix(t, "]")
		for _, p := range strings.Split(t, ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
	}
	return strings.Join(parts, ",")
}

// remoteList backs `catalog list`, returning lightweight search hits by default
// and hydrating complete items only when requested.
func remoteList(opts catalogListOpts) ([]catalogItem, error) {
	client, err := mccontext.RemoteClient()
	if err != nil {
		return nil, err
	}

	agent := opts.Agent
	if agent == "" {
		agent = "all"
	}

	selector := clientapi.ResourceSelector{
		Search:      opts.Query,
		Agent:       agent,
		Namespace:   opts.Namespace,
		TagSelector: joinTagSelectors(opts.Tag),
	}
	if opts.Type != "" {
		selector.Types = strings.Split(opts.Type, ",")
	}
	limit := opts.Limit
	if limit == 0 {
		limit = 100
	}

	resp, err := client.SearchCatalog(context.Background(), clientapi.SearchResourcesRequest{
		Limit:      limit,
		Timestamps: true,
		Configs:    []clientapi.ResourceSelector{selector},
	})
	if err != nil {
		return nil, err
	}

	return catalogItemsFromSearch(context.Background(), client, resp.Configs, opts.Full)
}

func catalogItemsFromSearch(ctx context.Context, client *sdk.Client, items []clientapi.SelectedResource, full bool) ([]catalogItem, error) {
	if full {
		ids := make([]string, len(items))
		for i, item := range items {
			ids[i] = item.ID
		}
		fullItems, err := client.GetCatalogItems(ctx, ids)
		if err != nil {
			return nil, err
		}

		fullItemsByID := make(map[string]clientapi.ConfigItem, len(fullItems))
		for _, item := range fullItems {
			fullItemsByID[item.ID.String()] = item
		}

		out := make([]catalogItem, 0, len(items))
		for _, item := range items {
			if fullItem, ok := fullItemsByID[item.ID]; ok {
				out = append(out, catalogItem(fullItem))
			} else {
				out = append(out, selectedResourceToConfigItem(item))
			}
		}
		return out, nil
	}

	out := make([]catalogItem, 0, len(items))
	for _, item := range items {
		out = append(out, selectedResourceToConfigItem(item))
	}
	return out, nil
}

func selectedResourceToConfigItem(s clientapi.SelectedResource) catalogItem {
	ci := clientapi.ConfigItem{ConfigClass: s.Type}
	if id, err := uuid.Parse(s.ID); err == nil {
		ci.ID = id
	}
	if s.Name != "" {
		name := s.Name
		ci.Name = &name
	}
	if s.Type != "" {
		typ := s.Type
		ci.Type = &typ
	}
	if s.Status != "" {
		status := s.Status
		ci.Status = &status
	}
	if s.Health != "" {
		health := s.Health
		ci.Health = &health
	}
	if len(s.Tags) > 0 {
		ci.Tags = s.Tags
	}
	if s.CreatedAt != nil {
		ci.CreatedAt = *s.CreatedAt
	}
	ci.UpdatedAt = s.UpdatedAt
	ci.DeletedAt = s.DeletedAt
	return catalogItem(ci)
}

// remoteGet backs `catalog get <id>`. With --relationships it returns the
// relationship tree; otherwise the full config item.
func remoteGet(id string, flags map[string]string) (any, error) {
	client, err := mccontext.RemoteClient()
	if err != nil {
		return nil, err
	}
	if flags["relationships"] == "true" {
		relationships, err := client.GetCatalogRelationships(context.Background(), id)
		if err != nil {
			return nil, err
		}
		return catalogRelationshipsView(relationships), nil
	}
	ctx := context.Background()
	item, err := client.GetCatalogItem(ctx, id)
	if err != nil {
		return nil, err
	}
	summary, err := client.GetCatalogItemSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	return &catalogItemDetail{ConfigItem: *item, Summary: summary}, nil
}

func completeCatalogIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	items, err := remoteList(catalogListOpts{Limit: 20})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var ids []string
	for _, c := range items {
		id := c.ID.String()
		if toComplete == "" || strings.HasPrefix(id, toComplete) {
			ids = append(ids, id)
		}
	}
	return ids, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	CatalogList.Flags().StringVar(&catalogListOptions.Query, "query", "", "Free-form text or catalog query expression")
	CatalogList.Flags().StringVar(&catalogListOptions.Type, "type", "", "Filter by type (comma-separated, supports ! negation)")
	CatalogList.Flags().StringVar(&catalogListOptions.Namespace, "namespace", "", "Filter by namespace")
	CatalogList.Flags().StringSliceVar(&catalogListOptions.Tag, "tag", nil, "Filter by tag as a label selector (repeatable: --tag cluster=foo)")
	CatalogList.Flags().StringVar(&catalogListOptions.Agent, "agent", "all", "Filter by agent id or name ('all' for every agent)")
	CatalogList.Flags().IntVar(&catalogListOptions.Limit, "limit", 100, "Maximum number of results")
	CatalogList.Flags().BoolVar(&catalogListOptions.Full, "full", false, "Return complete catalog items")
	CatalogGet.Flags().BoolVar(&catalogGetOptions.Relationships, "relationships", false, "Return the config relationship tree instead of the item")
	Catalog.AddCommand(CatalogList, CatalogGet)
}
