package cmd

import (
	"encoding/json"
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
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

var catalogGetSince string

var Get = &cobra.Command{
	Use:              "get <ID|QUERY>",
	Short:            "Get a full config item with relationships, insights, changes, access, and playbook runs",
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			return err
		}
		defer stop()

		result, err := runCatalogGet(ctx, args, catalogGetSince)
		if err != nil {
			return err
		}

		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		return nil
	},
}

func resolveConfigID(ctx context.Context, args []string) (*models.ConfigItem, error) {
	configs, err := resolveConfigs(ctx, args, 2)
	if err != nil {
		return nil, err
	}
	if len(configs) > 1 {
		return nil, fmt.Errorf("query matched multiple configs, expected exactly one")
	}
	return &configs[0], nil
}

func resolveConfigs(ctx context.Context, args []string, limit int) ([]models.ConfigItem, error) {
	if id, err := uuid.Parse(args[0]); err == nil {
		config, err := query.GetCachedConfig(ctx, id.String())
		if err != nil {
			return nil, fmt.Errorf("failed to get config %s: %w", id, err)
		}
		if config == nil {
			return nil, fmt.Errorf("config item %s not found", id)
		}
		return []models.ConfigItem{*config}, nil
	}

	req := parseQuery(args)
	if limit > 0 {
		req.Limit = limit
	}
	response, err := query.SearchResources(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	if len(response.Configs) == 0 {
		return nil, fmt.Errorf("no config found matching query")
	}

	var configs []models.ConfigItem
	for _, c := range response.Configs {
		config, err := query.GetCachedConfig(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get config %s: %w", c.ID, err)
		}
		if config != nil {
			configs = append(configs, *config)
		}
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("no config found matching query")
	}
	return configs, nil
}

type CatalogGetResult struct {
	models.ConfigItem `json:",inline"`
	LastScrapedTime   *time.Time                   `json:"last_scraped_time,omitempty"`
	Related           []query.RelatedConfig        `json:"related,omitempty"`
	Insights          []models.ConfigAnalysis      `json:"insights,omitempty"`
	Changes           []models.ConfigChange        `json:"changes,omitempty"`
	Access            []models.ConfigAccessSummary `json:"access,omitempty"`
	PlaybookRuns      []models.PlaybookRun         `json:"playbook_runs,omitempty"`

	since string
}

func runCatalogGet(ctx context.Context, args []string, sinceStr string) (*CatalogGetResult, error) {
	config, err := resolveConfigID(ctx, args)
	if err != nil {
		return nil, err
	}

	since, err := duration.ParseDuration(sinceStr)
	if err != nil {
		return nil, fmt.Errorf("invalid --since value %q: %w", sinceStr, err)
	}
	sinceTime := time.Now().Add(-time.Duration(since))
	id := config.ID

	result := &CatalogGetResult{ConfigItem: *config, since: sinceStr}

	var lastScraped models.ConfigItemLastScrapedTime
	if err := ctx.DB().Where("config_id = ?", id).First(&lastScraped).Error; err == nil {
		result.LastScrapedTime = lastScraped.LastScrapedTime
	}

	result.Related, err = query.GetRelatedConfigs(ctx, query.RelationQuery{ID: id})
	if err != nil {
		return nil, fmt.Errorf("failed to get related configs: %w", err)
	}

	changesResp, err := query.FindCatalogChanges(ctx, query.CatalogChangesSearchRequest{
		BaseCatalogSearch: query.BaseCatalogSearch{
			CatalogID: id.String(),
			FromTime:  &sinceTime,
			PageSize:  50,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get changes: %w", err)
	}
	result.Changes = make([]models.ConfigChange, len(changesResp.Changes))
	for i, c := range changesResp.Changes {
		result.Changes[i] = models.ConfigChange{
			ID:         c.ID,
			ConfigID:   c.ConfigID,
			ChangeType: c.ChangeType,
			Severity:   models.Severity(c.Severity),
			Source:     c.Source,
			Summary:    c.Summary,
			Count:      c.Count,
			CreatedAt:  c.CreatedAt,
			CreatedBy:  c.CreatedBy,
		}
	}

	insightsResp, err := query.FindCatalogInsights(ctx, query.CatalogInsightsSearchRequest{
		BaseCatalogSearch: query.BaseCatalogSearch{
			CatalogID: id.String(),
			PageSize:  50,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get insights: %w", err)
	}
	result.Insights = insightsResp.Insights

	result.Access, err = query.FindConfigAccessByConfigIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, fmt.Errorf("failed to get access: %w", err)
	}

	if err := ctx.DB().Where("config_id = ? AND created_at >= ?", id, sinceTime).
		Order("created_at DESC").Limit(50).
		Find(&result.PlaybookRuns).Error; err != nil {
		return nil, fmt.Errorf("failed to get playbook runs: %w", err)
	}

	return result, nil
}

func (r CatalogGetResult) Pretty() api.Text {
	t := r.ConfigItem.Pretty()
	t = t.NewLine().Append(buildDetailsSection(r))

	if r.ConfigItem.Config != nil && *r.ConfigItem.Config != "" {
		t = t.NewLine().Append(clicky.Collapsed("Config", configCodeBlock(*r.ConfigItem.Config)))
	}

	if len(r.Related) > 0 {
		tree := buildRelationshipTree(&r.ConfigItem, r.Related)
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Relationships (%d)", len(r.Related)),
			tree,
		))
	}

	sinceLabel := r.since

	if len(r.Insights) > 0 {
		rows := lo.Map(r.Insights, func(a models.ConfigAnalysis, _ int) analysisRow {
			return analysisRow{a}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Open Insights (%d)", len(rows)),
			api.NewTableFrom(rows),
		))
	}

	if len(r.Changes) > 0 {
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Changes since %s (%d)", sinceLabel, len(r.Changes)),
			api.NewTableFrom(r.Changes),
		))
	}

	if len(r.Access) > 0 {
		rows := lo.Map(r.Access, func(a models.ConfigAccessSummary, _ int) accessRow {
			return accessRow{a}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Access (%d)", len(rows)),
			api.NewTableFrom(rows),
		))
	}

	if len(r.PlaybookRuns) > 0 {
		rows := lo.Map(r.PlaybookRuns, func(p models.PlaybookRun, _ int) playbookRunRow {
			return playbookRunRow{p}
		})
		t = t.NewLine().Append(clicky.Collapsed(
			fmt.Sprintf("Playbook Runs since %s (%d)", sinceLabel, len(rows)),
			api.NewTableFrom(rows),
		))
	}

	return t
}

func buildDetailsSection(r CatalogGetResult) api.DescriptionList {
	c := &r.ConfigItem
	items := []api.KeyValuePair{
		{Key: "ID", Value: c.ID.String()},
		{Key: "Type", Value: lo.FromPtrOr(c.Type, "-")},
		{Key: "Class", Value: c.ConfigClass},
	}

	if c.Health != nil {
		items = append(items, api.KeyValuePair{Key: "Health", Value: c.Health.Pretty()})
	}
	if c.Status != nil {
		items = append(items, api.KeyValuePair{Key: "Status", Value: *c.Status})
	}
	if c.Description != nil && *c.Description != "" {
		items = append(items, api.KeyValuePair{Key: "Description", Value: *c.Description})
	}
	if c.Source != nil && *c.Source != "" {
		items = append(items, api.KeyValuePair{Key: "Source", Value: *c.Source})
	}
	if c.ScraperID != nil && *c.ScraperID != "" {
		items = append(items, api.KeyValuePair{Key: "Scraper", Value: *c.ScraperID})
	}
	if r.LastScrapedTime != nil {
		items = append(items, api.KeyValuePair{Key: "Last Scraped", Value: api.Human(time.Since(*r.LastScrapedTime), "text-gray-600")})
	}
	if c.AgentID != uuid.Nil {
		items = append(items, api.KeyValuePair{Key: "Agent", Value: c.AgentID.String()})
	}
	if c.Ready {
		items = append(items, api.KeyValuePair{Key: "Ready", Value: "true"})
	}
	if c.Path != "" {
		items = append(items, api.KeyValuePair{Key: "Path", Value: c.Path})
	}
	if c.ParentID != nil {
		items = append(items, api.KeyValuePair{Key: "Parent", Value: c.ParentID.String()})
	}
	if len(c.ExternalID) > 0 {
		items = append(items, api.KeyValuePair{Key: "External ID", Value: strings.Join(c.ExternalID, ", ")})
	}

	if c.CostTotal30d > 0 {
		items = append(items, api.KeyValuePair{Key: "Cost (30d)", Value: fmt.Sprintf("$%.2f", c.CostTotal30d)})
	}

	if !c.CreatedAt.IsZero() {
		items = append(items, api.KeyValuePair{Key: "Created", Value: api.Human(time.Since(c.CreatedAt), "text-gray-600")})
	}
	if c.UpdatedAt != nil {
		items = append(items, api.KeyValuePair{Key: "Updated", Value: api.Human(time.Since(*c.UpdatedAt), "text-gray-600")})
	}
	if c.DeletedAt != nil {
		items = append(items, api.KeyValuePair{Key: "Deleted", Value: api.Human(time.Since(*c.DeletedAt), "text-red-600")})
		if c.DeleteReason != "" {
			items = append(items, api.KeyValuePair{Key: "Delete Reason", Value: c.DeleteReason})
		}
	}

	if c.Labels != nil && len(*c.Labels) > 0 {
		items = append(items, api.KeyValuePair{Key: "Labels", Value: clicky.Map(*c.Labels, "text-xs")})
	}
	if len(c.Tags) > 0 {
		items = append(items, api.KeyValuePair{Key: "Tags", Value: clicky.Map(c.Tags, "text-xs")})
	}

	if c.Properties != nil {
		for _, p := range *c.Properties {
			val := p.Text
			if val == "" && p.Value != nil {
				val = fmt.Sprintf("%d", *p.Value)
			}
			if val == "" {
				continue
			}
			label := p.Label
			if label == "" {
				label = p.Name
			}
			items = append(items, api.KeyValuePair{Key: label, Value: val})
		}
	}

	return api.DescriptionList{Items: items}
}

func configCodeBlock(configJSON string) api.Code {
	var parsed any
	if err := json.Unmarshal([]byte(configJSON), &parsed); err == nil {
		if pretty, err := json.MarshalIndent(parsed, "", "  "); err == nil {
			configJSON = string(pretty)
		}
	}
	return api.CodeBlock("json", configJSON)
}

// relatedConfigNode implements api.TreeNode for relationship tree rendering.
type relatedConfigNode struct {
	label    api.Text
	children []api.TreeNode
}

func (n relatedConfigNode) Pretty() api.Text            { return n.label }
func (n relatedConfigNode) GetChildren() []api.TreeNode { return n.children }

func buildRelationshipTree(config *models.ConfigItem, related []query.RelatedConfig) api.TextTree {
	// Index all nodes by ID
	nodes := make(map[string]*relatedConfigNode)
	rootID := config.ID.String()
	nodes[rootID] = &relatedConfigNode{label: config.Pretty()}

	for _, rc := range related {
		nodes[rc.ID.String()] = &relatedConfigNode{label: relatedConfigLabel(rc)}
	}

	// Build parent-child edges from Path (format: "grandparent.parent.child")
	for _, rc := range related {
		parentID := parentIDFromPath(rc.Path, rc.ID.String())
		if parentID == "" {
			parentID = rootID
		}
		if parent, ok := nodes[parentID]; ok {
			parent.children = append(parent.children, nodes[rc.ID.String()])
		} else {
			// parent not in result set, attach to root
			nodes[rootID].children = append(nodes[rootID].children, nodes[rc.ID.String()])
		}
	}

	return api.NewTree[api.TreeNode](nodes[rootID])
}

// parentIDFromPath extracts the parent ID from a dot-separated path.
// For path "a.b.c" and id "c", returns "b".
func parentIDFromPath(path, id string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(path, ".")
	for i, seg := range segments {
		if seg == id && i > 0 {
			return segments[i-1]
		}
	}
	return ""
}

func relatedConfigLabel(rc query.RelatedConfig) api.Text {
	t := clicky.Text("")
	if rc.Health != nil {
		t = t.Add(rc.Health.Pretty()).AddText(" ")
	}
	t = t.AddText(rc.Name, "font-bold")
	t = t.AddText(" ").Add(clicky.Text(rc.Type, "text-xs text-gray-600").Wrap("(", ")"))
	if rc.Status != nil && *rc.Status != "" {
		t = t.AddText(" ").Add(clicky.Text(*rc.Status, "text-xs text-gray-500"))
	}
	return t
}

func formatDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd", days)
	}
	return d.String()
}

// analysisRow wraps ConfigAnalysis for TableProvider.
type analysisRow struct {
	models.ConfigAnalysis
}

func (r analysisRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("Severity").Build(),
		api.Column("Type").Build(),
		api.Column("Analyzer").Build(),
		api.Column("Summary").Build(),
		api.Column("Status").Build(),
	}
}

func (r analysisRow) Row() map[string]any {
	return map[string]any{
		"Severity": r.ConfigAnalysis.Severity.Pretty(),
		"Type":     r.ConfigAnalysis.AnalysisType.Pretty(),
		"Analyzer": clicky.Text(r.Analyzer, "font-bold"),
		"Summary":  clicky.Text(r.Summary, "text-gray-700"),
		"Status":   clicky.Text(r.ConfigAnalysis.Status, "text-blue-600"),
	}
}

// accessRow wraps ConfigAccessSummary for TableProvider.
type accessRow struct {
	models.ConfigAccessSummary
}

func (r accessRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("User").Build(),
		api.Column("Role").Build(),
		api.Column("Email").Build(),
		api.Column("UserType").Label("Type").Build(),
		api.Column("LastSignedIn").Label("Last Signed In").Build(),
	}
}

func (r accessRow) Row() map[string]any {
	lastSignedIn := clicky.Text("-", "text-gray-400")
	if r.LastSignedInAt != nil {
		lastSignedIn = api.Human(time.Since(*r.LastSignedInAt), "text-gray-600")
	}
	return map[string]any{
		"User":         clicky.Text(r.User, "font-bold"),
		"Role":         clicky.Text(r.Role),
		"Email":        clicky.Text(r.Email, "text-gray-600"),
		"UserType":     clicky.Text(r.UserType, "text-gray-500"),
		"LastSignedIn": lastSignedIn,
	}
}

// playbookRunRow wraps PlaybookRun for TableProvider.
type playbookRunRow struct {
	models.PlaybookRun
}

func (r playbookRunRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("Status").Build(),
		api.Column("ID").Build(),
		api.Column("Duration").Build(),
		api.Column("CreatedAt").Label("Created").Build(),
		api.Column("Error").Build(),
	}
}

func (r playbookRunRow) Row() map[string]any {
	row := map[string]any{
		"Status":    r.PlaybookRun.Status.Pretty(),
		"ID":        clicky.Text(r.PlaybookRun.ID.String()[:8], "font-mono text-xs"),
		"Duration":  clicky.Text("-", "text-gray-400"),
		"CreatedAt": api.Human(time.Since(r.CreatedAt), "text-gray-600"),
		"Error":     clicky.Text(""),
	}

	if r.StartTime != nil && r.EndTime != nil {
		row["Duration"] = api.Human(r.EndTime.Sub(*r.StartTime), "text-gray-600")
	} else if r.StartTime != nil {
		row["Duration"] = api.Human(time.Since(*r.StartTime), "text-blue-600")
	}

	if r.PlaybookRun.Error != nil && *r.PlaybookRun.Error != "" {
		row["Error"] = clicky.Text(*r.PlaybookRun.Error, "text-red-600 text-sm")
	}

	return row
}
