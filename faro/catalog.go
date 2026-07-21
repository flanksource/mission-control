package main

import (
	"context"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/sdk"
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

func (catalogGetFlags) ClickyActionFlags() {}

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
func remoteList(opts catalogListOpts) ([]models.ConfigItem, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}

	agent := opts.Agent
	if agent == "" {
		agent = "all"
	}

	selector := types.ResourceSelector{
		Search:      opts.Query,
		Agent:       agent,
		Namespace:   opts.Namespace,
		TagSelector: joinTagSelectors(opts.Tag),
	}
	if opts.Type != "" {
		selector.Types = types.Items(strings.Split(opts.Type, ","))
	}
	limit := opts.Limit
	if limit == 0 {
		limit = 100
	}

	resp, err := client.SearchCatalog(context.Background(), query.SearchResourcesRequest{
		Limit:      limit,
		Timestamps: true,
		Configs:    []types.ResourceSelector{selector},
	})
	if err != nil {
		return nil, err
	}

	return catalogItemsFromSearch(context.Background(), client, resp.Configs, opts.Full)
}

func catalogItemsFromSearch(ctx context.Context, client *sdk.Client, items []query.SelectedResource, full bool) ([]models.ConfigItem, error) {
	if full {
		ids := make([]string, len(items))
		for i, item := range items {
			ids[i] = item.ID
		}
		return client.GetCatalogItems(ctx, ids)
	}

	out := make([]models.ConfigItem, 0, len(items))
	for _, item := range items {
		out = append(out, selectedResourceToConfigItem(item))
	}
	return out, nil
}

func selectedResourceToConfigItem(s query.SelectedResource) models.ConfigItem {
	ci := models.ConfigItem{ConfigClass: s.Type}
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
		health := models.Health(s.Health)
		ci.Health = &health
	}
	if len(s.Tags) > 0 {
		ci.Tags = types.JSONStringMap(s.Tags)
	}
	if s.CreatedAt != nil {
		ci.CreatedAt = *s.CreatedAt
	}
	ci.UpdatedAt = s.UpdatedAt
	ci.DeletedAt = s.DeletedAt
	return ci
}

// remoteGet backs `catalog get <id>`. With --relationships it returns the
// relationship tree; otherwise the full config item.
func remoteGet(id string, flags map[string]string) (any, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}
	if flags["relationships"] == "true" {
		return client.GetCatalogRelationships(context.Background(), id)
	}
	item, err := client.GetCatalogItem(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &catalogItemDetail{ConfigItem: *item}, nil
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
	clicky.RegisterEntity(clicky.Entity[models.ConfigItem, catalogListOpts, any]{
		Name:         "catalog",
		Aliases:      []string{"configs"},
		List:         remoteList,
		GetFlags:     catalogGetFlags{},
		GetWithFlags: remoteGet,
		ValidArgs:    completeCatalogIDs,
	})
}
