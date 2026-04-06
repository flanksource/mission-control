package catalog_report

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

type Options struct {
	Title           string
	Since           time.Duration
	Sections        api.CatalogReportSections
	Recursive       bool
	GroupBy         string // "merged" (default) or "config"
	ChangeArtifacts bool
}

func (o Options) WithDefaults() Options {
	if o.Since == 0 {
		o.Since = 30 * 24 * time.Hour
	}
	if o.Title == "" {
		o.Title = "Catalog Report"
	}
	if o.GroupBy == "" {
		o.GroupBy = "merged"
	}
	return o
}

func BuildReport(ctx context.Context, configs []models.ConfigItem, opts Options) (*api.CatalogReport, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no config items provided")
	}
	opts = opts.WithDefaults()
	sinceTime := time.Now().Add(-opts.Since)

	report := &api.CatalogReport{
		Title:       opts.Title,
		GeneratedAt: time.Now(),
		From:        sinceTime.Format(time.RFC3339),
		ConfigItem:  configs[0],
		Sections:    opts.Sections,
		Recursive:   opts.Recursive,
		GroupBy:     opts.GroupBy,
	}

	report.Parents = resolveParents(ctx, &configs[0])

	for _, config := range configs {
		entry, err := buildEntry(ctx, &config, opts, sinceTime)
		if err != nil {
			return nil, fmt.Errorf("failed to build entry for %s: %w", config.GetName(), err)
		}
		report.Entries = append(report.Entries, *entry)

		report.Changes = append(report.Changes, entry.Changes...)
		report.Analyses = append(report.Analyses, entry.Analyses...)
		report.Access = append(report.Access, entry.Access...)
		report.AccessLogs = append(report.AccessLogs, entry.AccessLogs...)
	}

	if opts.Sections.ConfigJSON && configs[0].Config != nil {
		report.ConfigJSON = configs[0].Config
	}

	if opts.GroupBy == "config" {
		report.Changes = nil
		report.Analyses = nil
		report.Access = nil
		report.AccessLogs = nil
	}

	return report, nil
}

func buildEntry(ctx context.Context, config *models.ConfigItem, opts Options, sinceTime time.Time) (*api.CatalogReportEntry, error) {
	entry := &api.CatalogReportEntry{
		ConfigItem: api.NewCatalogReportConfigItem(*config),
	}

	parents := resolveParents(ctx, config)
	entry.Parents = lo.Map(parents, func(p models.ConfigItem, _ int) api.CatalogReportConfigItem {
		return api.NewCatalogReportConfigItem(p)
	})

	tree, err := query.ConfigTree(ctx, config.ID, query.ConfigTreeOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to build config tree: %w", err)
	}

	targetIDs := tree.OutgoingIDs()
	configMap := make(map[uuid.UUID]models.ConfigItem)
	items, err := query.GetConfigsByIDs(ctx, targetIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load config items: %w", err)
	}
	for _, ci := range items {
		configMap[ci.ID] = ci
	}

	configMeta := func(configID string) (string, string) {
		if id, err := uuid.Parse(configID); err == nil {
			if ci, ok := configMap[id]; ok {
				typ := ""
				if ci.Type != nil {
					typ = *ci.Type
				}
				return ci.GetName(), typ
			}
		}
		return "", ""
	}

	catalogIDsCSV := strings.Join(lo.Map(targetIDs, func(id uuid.UUID, _ int) string { return id.String() }), ",")

	if opts.Sections.Changes {
		resp, err := query.FindCatalogChanges(ctx, query.CatalogChangesSearchRequest{
			BaseCatalogSearch: query.BaseCatalogSearch{
				CatalogID: catalogIDsCSV,
				FromTime:  &sinceTime,
				PageSize:  500,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get changes: %w", err)
		}
		entry.Changes = lo.Map(resp.Changes, func(c query.ConfigChangeRow, _ int) api.CatalogReportChange {
			name, typ := configMeta(c.ConfigID)
			r := api.CatalogReportChange{
				ID:                c.ID,
				ConfigID:          c.ConfigID,
				ConfigName:        name,
				ConfigType:        typ,
				Permalink:         api.ConfigPermalink(c.ConfigID),
				ChangeType:        c.ChangeType,
				Severity:          c.Severity,
				Source:            c.Source,
				Summary:           c.Summary,
				ExternalCreatedBy: c.ExternalCreatedBy,
				Count:             c.Count,
			}
			if c.CreatedAt != nil {
				r.CreatedAt = c.CreatedAt.Format(time.RFC3339)
			}
			if c.CreatedBy != nil {
				r.CreatedBy = c.CreatedBy.String()
			}
			return r
		})
		entry.ChangeCount = len(entry.Changes)

		if opts.ChangeArtifacts && len(entry.Changes) > 0 {
			attachChangeArtifacts(ctx, entry.Changes)
		}
	}

	if opts.Sections.Insights {
		resp, err := query.FindCatalogInsights(ctx, query.CatalogInsightsSearchRequest{
			BaseCatalogSearch: query.BaseCatalogSearch{
				CatalogID: catalogIDsCSV,
				PageSize:  500,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get insights: %w", err)
		}
		entry.Analyses = lo.Map(resp.Insights, func(a models.ConfigAnalysis, _ int) api.CatalogReportAnalysis {
			name, typ := configMeta(a.ConfigID.String())
			return api.NewCatalogReportAnalysis(a, name, typ)
		})
		entry.InsightCount = len(entry.Analyses)
	}

	if opts.Sections.Access {
		rbacRows, err := db.GetRBACAccessByConfigIDs(ctx, targetIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get access: %w", err)
		}
		entry.RBACResources = groupRBACByConfig(rbacRows, configMap, opts)
		for _, r := range entry.RBACResources {
			entry.AccessCount += len(r.Users)
		}
	}

	if opts.Sections.AccessLogs {
		logs, err := getAccessLogs(ctx, targetIDs, sinceTime)
		if err != nil {
			return nil, fmt.Errorf("failed to get access logs: %w", err)
		}
		entry.AccessLogs = lo.Map(logs, func(l accessLogRow, _ int) api.CatalogReportAccessLog {
			return newAccessLogEntry(l)
		})
	}

	if opts.Sections.Relationships && tree != nil {
		entry.RelationshipTree = configTreeNodeToReport(tree)
	}

	return entry, nil
}

func buildConfigGroups(report *api.CatalogReport, configMap map[uuid.UUID]models.ConfigItem) []api.CatalogReportConfigGroup {
	changesByConfig := lo.GroupBy(report.Changes, func(c api.CatalogReportChange) string { return c.ConfigID })
	analysesByConfig := lo.GroupBy(report.Analyses, func(a api.CatalogReportAnalysis) string { return a.ConfigID })
	accessByConfig := lo.GroupBy(report.Access, func(a api.CatalogReportAccess) string { return a.ConfigID })
	logsByConfig := lo.GroupBy(report.AccessLogs, func(l api.CatalogReportAccessLog) string { return l.ConfigID })

	seen := make(map[string]bool)
	var groups []api.CatalogReportConfigGroup

	for _, id := range sortedConfigIDs(configMap) {
		idStr := id.String()
		if seen[idStr] {
			continue
		}
		seen[idStr] = true

		changes := changesByConfig[idStr]
		analyses := analysesByConfig[idStr]
		access := accessByConfig[idStr]
		logs := logsByConfig[idStr]

		if len(changes) == 0 && len(analyses) == 0 && len(access) == 0 && len(logs) == 0 {
			continue
		}

		ci := configMap[id]
		groups = append(groups, api.CatalogReportConfigGroup{
			ConfigItem: api.NewCatalogReportConfigItem(ci),
			Changes:    changes,
			Analyses:   analyses,
			Access:     access,
			AccessLogs: logs,
		})
	}
	return groups
}

func sortedConfigIDs(m map[uuid.UUID]models.ConfigItem) []uuid.UUID {
	ids := lo.Keys(m)
	slices.SortFunc(ids, func(a, b uuid.UUID) int {
		return strings.Compare(m[a].GetName(), m[b].GetName())
	})
	return ids
}

func resolveParents(ctx context.Context, config *models.ConfigItem) []models.ConfigItem {
	var parents []models.ConfigItem
	current := config
	for current.ParentID != nil {
		loaded, err := query.GetConfigsByIDs(ctx, []uuid.UUID{*current.ParentID})
		if err != nil || len(loaded) == 0 {
			break
		}
		parents = append(parents, loaded[0])
		current = &loaded[0]
	}
	for i, j := 0, len(parents)-1; i < j; i, j = i+1, j-1 {
		parents[i], parents[j] = parents[j], parents[i]
	}
	return parents
}

type accessLogRow struct {
	ConfigID       uuid.UUID      `gorm:"column:config_id"`
	ConfigName     string         `gorm:"column:config_name"`
	ConfigType     string         `gorm:"column:config_type"`
	ExternalUserID uuid.UUID      `gorm:"column:external_user_id"`
	UserName       string         `gorm:"column:user_name"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	MFA            bool           `gorm:"column:mfa"`
	Count          *int           `gorm:"column:count"`
	Properties     map[string]any `gorm:"column:properties;serializer:json"`
}

func (r accessLogRow) QueryLogSummary() string {
	return r.ConfigType
}

func getAccessLogs(ctx context.Context, configIDs []uuid.UUID, since time.Time) (results []accessLogRow, err error) {
	timer := query.NewQueryLogger(ctx).Start("AccessLogs").Arg("configIDs", len(configIDs))
	defer timer.End(&err)

	if err = ctx.DB().
		Table("config_access_logs").
		Select(`config_access_logs.config_id,
			config_items.name AS config_name,
			config_items.type AS config_type,
			config_access_logs.external_user_id,
			external_users.name AS user_name,
			config_access_logs.created_at,
			config_access_logs.mfa,
			config_access_logs.count,
			config_access_logs.properties`).
		Joins("JOIN config_items ON config_items.id = config_access_logs.config_id").
		Joins("JOIN external_users ON external_users.id = config_access_logs.external_user_id").
		Where("config_access_logs.config_id IN ?", configIDs).
		Where("config_access_logs.created_at >= ?", since).
		Order("config_access_logs.created_at DESC").
		Scan(&results).Error; err != nil {
		return nil, err
	}
	timer.Results(results)
	return results, nil
}

func newAccessLogEntry(r accessLogRow) api.CatalogReportAccessLog {
	var props map[string]string
	if r.Properties != nil {
		props = make(map[string]string, len(r.Properties))
		for k, v := range r.Properties {
			props[k] = fmt.Sprintf("%v", v)
		}
	}
	return api.CatalogReportAccessLog{
		ConfigID:   r.ConfigID.String(),
		Permalink:  api.ConfigPermalink(r.ConfigID.String()),
		UserID:     r.ExternalUserID.String(),
		UserName:   r.UserName,
		ConfigName: r.ConfigName,
		ConfigType: r.ConfigType,
		CreatedAt:  r.CreatedAt.Format(time.RFC3339),
		MFA:        r.MFA,
		Count:      lo.FromPtr(r.Count),
		Properties: props,
	}
}

func attachChangeArtifacts(ctx context.Context, changes []api.CatalogReportChange) {
	changeIDs := make([]uuid.UUID, 0, len(changes))
	for _, c := range changes {
		if id, err := uuid.Parse(c.ID); err == nil {
			changeIDs = append(changeIDs, id)
		}
	}
	if len(changeIDs) == 0 {
		return
	}

	var artifacts []models.Artifact
	if err := ctx.DB().Where("config_change_id IN ?", changeIDs).Find(&artifacts).Error; err != nil {
		ctx.Logger.V(2).Infof("failed to query change artifacts: %v", err)
		return
	}
	if len(artifacts) == 0 {
		return
	}

	byChangeID := lo.GroupBy(artifacts, func(a models.Artifact) string {
		if a.ConfigChangeID != nil {
			return a.ConfigChangeID.String()
		}
		return ""
	})

	for i := range changes {
		arts, ok := byChangeID[changes[i].ID]
		if !ok {
			continue
		}
		for _, a := range arts {
			ra := api.CatalogReportArtifact{
				ID:          a.ID.String(),
				Filename:    a.Filename,
				ContentType: a.ContentType,
				Size:        a.Size,
			}
			if isEmbeddableContentType(a.ContentType) {
				if data, err := a.GetContent(); err == nil && len(data) > 0 {
					ra.DataURI = fmt.Sprintf("data:%s;base64,%s", a.ContentType, base64.StdEncoding.EncodeToString(data))
				}
			}
			changes[i].Artifacts = append(changes[i].Artifacts, ra)
		}
	}
}

func isEmbeddableContentType(ct string) bool {
	for _, prefix := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/svg"} {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}

func groupRBACByConfig(rows []db.RBACAccessRow, configMap map[uuid.UUID]models.ConfigItem, opts Options) []api.RBACResource {
	staleThreshold := time.Now().AddDate(0, 0, -90)
	reviewThreshold := time.Now().AddDate(0, 0, -90)

	grouped := make(map[uuid.UUID]*api.RBACResource)
	var order []uuid.UUID

	for _, row := range rows {
		resource, ok := grouped[row.ConfigID]
		if !ok {
			resource = &api.RBACResource{
				ConfigID:   row.ConfigID.String(),
				ConfigName: row.ConfigName,
				ConfigType: row.ConfigType,
			}
			if ci, found := configMap[row.ConfigID]; found {
				resource.ConfigClass = ci.ConfigClass
				resource.Path = ci.Path
				if ci.Status != nil {
					resource.Status = *ci.Status
				}
				if ci.Health != nil {
					resource.Health = string(*ci.Health)
				}
				resource.Tags = ci.Tags
				if ci.Labels != nil {
					resource.Labels = *ci.Labels
				}
			}
			grouped[row.ConfigID] = resource
			order = append(order, row.ConfigID)
		}

		resource.Users = append(resource.Users, api.RBACUserRole{
			UserID:          row.UserID.String(),
			UserName:        row.UserName,
			Email:           row.Email,
			Role:            row.Role,
			RoleSource:      row.RoleSource(),
			SourceSystem:    row.UserType,
			CreatedAt:       row.CreatedAt,
			LastSignedInAt:  row.LastSignedInAt,
			LastReviewedAt:  row.LastReviewedAt,
			IsStale:         row.LastSignedInAt == nil || row.LastSignedInAt.Before(staleThreshold),
			IsReviewOverdue: row.LastReviewedAt == nil || row.LastReviewedAt.Before(reviewThreshold),
		})
	}

	return lo.Map(order, func(id uuid.UUID, _ int) api.RBACResource {
		return *grouped[id]
	})
}

func configTreeNodeToReport(n *query.ConfigTreeNode) *api.CatalogReportTreeNode {
	result := &api.CatalogReportTreeNode{
		CatalogReportConfigItem: api.NewCatalogReportConfigItem(n.ConfigItem),
		EdgeType:                n.EdgeType,
		Relation:                n.Relation,
	}
	for _, c := range n.Children {
		result.Children = append(result.Children, *configTreeNodeToReport(c))
	}
	return result
}

func RelatedConfigToReportItem(rc query.RelatedConfig) api.CatalogReportConfigItem {
	r := api.CatalogReportConfigItem{
		ID:        rc.ID.String(),
		Permalink: api.ConfigPermalink(rc.ID.String()),
		Name:      rc.Name,
		Type:      rc.Type,
		Tags:      rc.Tags,
	}
	if rc.Status != nil {
		r.Status = *rc.Status
	}
	if rc.Health != nil {
		r.Health = string(*rc.Health)
	}
	if !rc.CreatedAt.IsZero() {
		r.CreatedAt = rc.CreatedAt.Format(time.RFC3339)
	}
	if !rc.UpdatedAt.IsZero() {
		r.UpdatedAt = rc.UpdatedAt.Format(time.RFC3339)
	}
	return r
}
