package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/spf13/cobra"
)

// joinTagSelectors flattens the tag slice into a comma-separated
// Kubernetes label selector string. Clicky's current entity generation
// round-trips `[]string` flag values through `pflag.Value.String()`,
// which produces `[k=v,x=y]` and is then re-split on commas — leaving
// stray `[` on the first element and `]` on the last. We strip those
// boundary characters so users get predictable semantics across
// `--tag k=v` and `--tag k=v --tag x=y`.
func joinTagSelectors(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "[")
		t = strings.TrimSuffix(t, "]")
		for _, p := range strings.Split(t, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				parts = append(parts, p)
			}
		}
	}
	return strings.Join(parts, ",")
}

// catalogListOpts captures all filter flags for the `catalog list` command.
// Fields are bound to CLI flags by clicky via the `flag:"..."` tags.
type catalogListOpts struct {
	Query     string   `flag:"query" help:"Free-form search (name/tag/labels, PEG query language)"`
	Type      string   `flag:"type" help:"Filter by type (comma-separated, supports ! negation e.g. Kubernetes::Pod,!Kubernetes::Node)"`
	Status    string   `flag:"status" help:"Filter by status (comma-separated)"`
	Health    string   `flag:"health" help:"Filter by health (healthy, unhealthy, warning, unknown; comma-separated)"`
	Deleted   bool     `flag:"deleted" help:"Include soft-deleted configs"`
	Tag       []string `flag:"tag" help:"Filter by tag as Kubernetes label selector (repeatable: --tag cluster=foo --tag env!=prod)"`
	Agent     string   `flag:"agent" help:"Filter by agent id or name ('self' for local)"`
	Namespace string   `flag:"namespace" help:"Filter by namespace"`
	Limit     int      `flag:"limit" help:"Maximum number of results" default:"100"`
}

// listConfigs backs `catalog list`. It opens a duty client context, builds a
// ResourceSelector from the filter flags, and calls the cached config finder.
func listConfigs(opts catalogListOpts) ([]models.ConfigItem, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}

	selector := types.ResourceSelector{
		Cache:          "no-cache",
		Search:         opts.Query,
		IncludeDeleted: opts.Deleted,
		Agent:          opts.Agent,
		Namespace:      opts.Namespace,
		TagSelector:    joinTagSelectors(opts.Tag),
	}
	if opts.Type != "" {
		selector.Types = types.Items(strings.Split(opts.Type, ","))
	}
	if opts.Status != "" {
		selector.Statuses = types.Items(strings.Split(opts.Status, ","))
	}
	if opts.Health != "" {
		selector.Health = types.MatchExpression(opts.Health)
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 100
	}

	configs, err := query.FindConfigsByResourceSelector(ctx, limit, selector)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	return configs, nil
}

// catalogGetFlags mirrors the original `catalog get` cobra flags so the
// entity Get action keeps 1:1 behavioural parity.
type catalogGetFlags struct {
	Since         string `flag:"since" help:"Time range for changes, playbook runs, access logs (7d, 2w, 30d)" default:"7d"`
	Changes       bool   `flag:"changes" help:"Include config changes section"`
	Insights      bool   `flag:"insights" help:"Include config insights section"`
	Access        bool   `flag:"access" help:"Include RBAC access section"`
	AccessLogs    bool   `flag:"access-logs" help:"Include access logs section"`
	Relationships bool   `flag:"relationships" help:"Include relationships section" default:"true"`
	ConfigJSON    bool   `flag:"config-json" help:"Include raw config JSON" default:"true"`
	PlaybookRuns  bool   `flag:"playbook-runs" help:"Include playbook runs section" default:"true"`
	Direction     string `flag:"direction" help:"Relationship direction: all, incoming, outgoing" default:"all"`
	Depth         int    `flag:"depth" help:"Relationship traversal depth (0 = default 5)"`
	All           bool   `flag:"all" help:"Include all sections (overrides individual section flags)"`
}

func (catalogGetFlags) ClickyActionFlags() {}

// getConfig backs `catalog get <id>`. It resolves the id/query, fetches the
// config, and builds the full result (relationships, insights, etc.) using
// the shared helpers in catalog_get.go.
func getConfig(id string, flags map[string]string) (any, error) {
	ctx, err := startDutyClient()
	if err != nil {
		return nil, err
	}

	opts, err := buildCatalogGetOptionsFromFlags(flags)
	if err != nil {
		return nil, err
	}

	configs, err := resolveConfigsForCommand(ctx, []string{id})
	if err != nil {
		return nil, err
	}

	since, err := duration.ParseDuration(opts.Since)
	if err != nil {
		return nil, fmt.Errorf("invalid --since value %q: %w", opts.Since, err)
	}
	sinceTime := time.Now().Add(-time.Duration(since))

	if len(configs) == 1 {
		return buildCatalogGetResult(ctx, &configs[0], opts, sinceTime)
	}

	results := make([]CatalogGetResult, 0, len(configs))
	for i := range configs {
		r, err := buildCatalogGetResult(ctx, &configs[i], opts, sinceTime)
		if err != nil {
			return nil, err
		}
		results = append(results, *r)
	}
	return &CatalogGetResults{Results: results}, nil
}

// startDutyClient opens a client-only duty context. It is called by the entity
// handlers so we don't rely on a cobra-attached RunE for DB init.
func startDutyClient() (context.Context, error) {
	logger.UseSlog()
	if err := properties.LoadFile("mission-control.properties"); err != nil {
		logger.Errorf(err.Error())
	}
	ctx, _, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return context.Context{}, err
	}
	return ctx, nil
}

// Entity filters. Options() returns nil because the valid values are
// open-ended (types, statuses, healths are driven by the data). Lookup()
// echoes the user's selection so the UI/OpenAPI surface reports the
// currently applied filter.

type catalogTypeFilter struct{}

func (catalogTypeFilter) Key() string   { return "type" }
func (catalogTypeFilter) Label() string { return "Type" }
func (catalogTypeFilter) Lookup(opts *catalogListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Type), nil
}
func (catalogTypeFilter) Options(_ catalogListOpts) map[string]api.Textable { return nil }

type catalogStatusFilter struct{}

func (catalogStatusFilter) Key() string   { return "status" }
func (catalogStatusFilter) Label() string { return "Status" }
func (catalogStatusFilter) Lookup(opts *catalogListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Status), nil
}
func (catalogStatusFilter) Options(_ catalogListOpts) map[string]api.Textable { return nil }

type catalogHealthFilter struct{}

func (catalogHealthFilter) Key() string   { return "health" }
func (catalogHealthFilter) Label() string { return "Health" }
func (catalogHealthFilter) Lookup(opts *catalogListOpts) (map[string]api.Textable, error) {
	return echoFilterLookup(opts.Health), nil
}
func (catalogHealthFilter) Options(_ catalogListOpts) map[string]api.Textable {
	return map[string]api.Textable{
		"healthy":   api.Text{Content: "Healthy", Style: "text-green-600"},
		"unhealthy": api.Text{Content: "Unhealthy", Style: "text-red-600"},
		"warning":   api.Text{Content: "Warning", Style: "text-amber-600"},
		"unknown":   api.Text{Content: "Unknown", Style: "text-gray-500"},
	}
}

type catalogTagFilter struct{}

func (catalogTagFilter) Key() string   { return "tag" }
func (catalogTagFilter) Label() string { return "Tag" }
func (catalogTagFilter) Lookup(opts *catalogListOpts) (map[string]api.Textable, error) {
	if len(opts.Tag) == 0 {
		return nil, nil
	}
	out := make(map[string]api.Textable, len(opts.Tag))
	for _, t := range opts.Tag {
		out[t] = api.Text{Content: t, Style: "font-mono text-xs"}
	}
	return out, nil
}
func (catalogTagFilter) Options(_ catalogListOpts) map[string]api.Textable { return nil }

func echoFilterLookup(value string) map[string]api.Textable {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make(map[string]api.Textable, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out[p] = api.Text{Content: p}
	}
	return out
}

// completeConfigIDs provides shell completion for the <id> argument by
// running a short list via the default options.
func completeConfigIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	configs, err := listConfigs(catalogListOpts{Limit: 20})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var ids []string
	for _, c := range configs {
		id := c.ID.String()
		if toComplete == "" || strings.HasPrefix(id, toComplete) {
			ids = append(ids, id)
		}
	}
	return ids, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	clicky.RegisterEntity(clicky.Entity[models.ConfigItem, catalogListOpts, any]{
		Name:    "catalog",
		Aliases: []string{"configs"},
		Filters: []clicky.Filter[catalogListOpts]{
			catalogTypeFilter{},
			catalogStatusFilter{},
			catalogHealthFilter{},
			catalogTagFilter{},
		},
		List:         listConfigs,
		GetFlags:     catalogGetFlags{},
		GetWithFlags: getConfig,
		ValidArgs:    completeConfigIDs,
	})
}
