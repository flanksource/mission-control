package catalog

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

type Options struct {
	Title             string
	Since             time.Duration
	Sections          api.CatalogReportSections
	Recursive         bool
	GroupBy           string // "none" (default), "merged", or "config"
	ChangeArtifacts   bool
	Audit             bool
	ExpandGroups      bool
	Settings          *Settings
	SettingsPath      string
	IncludedConfigIDs map[uuid.UUID]bool

	// Limit caps the number of config items (including recursive descendants)
	// included in the report. 0 = unlimited.
	Limit int
	// MaxItems caps each per-entry section (changes, analyses, access-logs).
	// Access matrix rows are never capped. Section-specific overrides take
	// precedence. 0 = unlimited.
	MaxItems int
	// MaxChanges overrides MaxItems for the changes section. 0 = unlimited.
	MaxChanges int
	// MaxItemArtifacts caps the number of artifacts retained per change source
	// within a single catalog entry. 0 = unlimited.
	MaxItemArtifacts int
}

// effectiveMax resolves the cap for a section, taking the tighter of an
// optional section-specific override and the generic MaxItems floor. A return
// of 0 means "no cap".
func (o Options) effectiveMax(override int) int {
	switch {
	case override > 0 && o.MaxItems > 0:
		if override < o.MaxItems {
			return override
		}
		return o.MaxItems
	case override > 0:
		return override
	default:
		return o.MaxItems
	}
}

// pageSizeFor converts an effectiveMax result into a duty PageSize value.
// duty's BaseCatalogSearch.SetDefaults forces PageSize<=0 to 50, so "unlimited"
// must be expressed as a large sentinel.
func (o Options) pageSizeFor(override int) int {
	if n := o.effectiveMax(override); n > 0 {
		return n
	}
	return math.MaxInt32
}

func (o Options) StaleDays() int {
	if o.Settings != nil {
		return o.Settings.Thresholds.StaleDays
	}
	return 0
}

func (o Options) ReviewOverdueDays() int {
	if o.Settings != nil {
		return o.Settings.Thresholds.ReviewOverdueDays
	}
	return 0
}

func (o Options) WithDefaults() Options {
	if o.Since == 0 {
		o.Since = 30 * 24 * time.Hour
	}
	if o.Title == "" {
		o.Title = "Catalog Report"
	}
	if o.GroupBy == "" {
		o.GroupBy = "none"
	}
	return o
}

func BuildReport(ctx context.Context, configs []models.ConfigItem, opts Options) (*api.CatalogReport, []string, error) {
	if len(configs) == 0 {
		return nil, nil, fmt.Errorf("no config items provided")
	}
	opts = opts.WithDefaults()
	sinceTime := time.Now().Add(-opts.Since)
	var mappings []api.CatalogReportCategoryMapping
	if opts.Settings != nil {
		mappings = opts.Settings.CategoryMappings
	}
	mapper, err := newChangeMapper(ctx, mappings)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize change mappings: %w", err)
	}

	report := &api.CatalogReport{
		Title:       opts.Title,
		GeneratedAt: time.Now(),
		PublicURL:   api.FrontendURL,
		From:        sinceTime.Format(time.RFC3339),
		ConfigItem:  configs[0],
		Sections:    opts.Sections,
		Recursive:   opts.Recursive,
		GroupBy:     opts.GroupBy,
	}

	report.Parents = resolveParents(ctx, &configs[0])

	if opts.Limit > 0 && len(configs) > opts.Limit {
		configs = configs[:opts.Limit]
	}

	scraperIDSet := make(map[string]bool)
	for _, config := range configs {
		if config.ScraperID != nil && *config.ScraperID != "" {
			scraperIDSet[*config.ScraperID] = true
		}
	}

	for _, config := range configs {
		entry, entryScraperIDs, err := buildEntryWithMapper(ctx, &config, opts, sinceTime, mapper)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build entry for %s: %w", config.GetName(), err)
		}
		report.Entries = append(report.Entries, *entry)

		report.Changes = append(report.Changes, entry.Changes...)
		report.Analyses = append(report.Analyses, entry.Analyses...)
		report.Access = append(report.Access, entry.Access...)
		report.AccessLogs = append(report.AccessLogs, entry.AccessLogs...)

		for _, id := range entryScraperIDs {
			scraperIDSet[id] = true
		}
	}

	// Always stitch per-entry subtrees into a single ancestry tree so the
	// reader sees one unified tree, regardless of how many configs are in the
	// report. The per-entry trees already contain the related resources (loaded
	// by buildEntryWithMapper); we graft them onto their target node in the
	// unified tree so no DB work is repeated.
	if opts.Sections.Relationships {
		entryTrees := make(map[uuid.UUID]*api.CatalogReportTreeNode, len(report.Entries))
		for i := range report.Entries {
			e := report.Entries[i]
			if e.RelationshipTree == nil || e.ConfigItem.ID == "" {
				continue
			}
			id, err := uuid.Parse(e.ConfigItem.ID)
			if err != nil {
				continue
			}
			entryTrees[id] = e.RelationshipTree
		}
		unified, err := buildRecursiveRelationshipTree(ctx, configs, entryTrees)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build relationship tree: %w", err)
		}
		if unified != nil {
			report.RelationshipTree = unified
		}
	}

	if opts.Sections.ConfigJSON && configs[0].Config != nil {
		report.ConfigJSON = configs[0].Config
	}

	if opts.GroupBy == "none" && len(report.Entries) > 1 {
		classifyRootsAndBreadcrumbs(configs, report.Entries)
	}

	if opts.GroupBy == "config" {
		report.Changes = nil
		report.Analyses = nil
		report.Access = nil
		report.AccessLogs = nil
	}

	if opts.Settings != nil {
		if len(opts.Settings.CategoryMappings) > 0 {
			report.CategoryMappings = opts.Settings.CategoryMappings
		}
		report.Thresholds = &api.CatalogReportThresholds{
			StaleDays:         opts.StaleDays(),
			ReviewOverdueDays: opts.ReviewOverdueDays(),
		}
	}

	var scraperIDs []string
	for id := range scraperIDSet {
		scraperIDs = append(scraperIDs, id)
	}

	return report, scraperIDs, nil
}

func buildEntryWithMapper(ctx context.Context, config *models.ConfigItem, opts Options, sinceTime time.Time, mapper *changeMapper) (*api.CatalogReportEntry, []string, error) {
	entry := &api.CatalogReportEntry{
		ConfigItem: api.NewCatalogReportConfigItem(*config),
	}

	parents := resolveParents(ctx, config)
	entry.Parents = lo.Map(parents, func(p models.ConfigItem, _ int) api.CatalogReportConfigItem {
		return api.NewCatalogReportConfigItem(p)
	})

	var tree *query.ConfigTreeNode
	targetIDs := []uuid.UUID{config.ID}
	if opts.Recursive {
		var err error
		tree, err = query.ConfigTree(ctx, config.ID, query.ConfigTreeOptions{})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build config tree: %w", err)
		}

		targetIDs = tree.OutgoingIDs()
		if len(opts.IncludedConfigIDs) > 0 {
			targetIDs = lo.Filter(targetIDs, func(id uuid.UUID, _ int) bool {
				return opts.IncludedConfigIDs[id]
			})
		}
	} else if opts.Sections.Relationships {
		tree = &query.ConfigTreeNode{
			ConfigItem: *config,
			EdgeType:   "target",
		}
	}
	configMap := make(map[uuid.UUID]models.ConfigItem)
	items, err := query.GetConfigsByIDs(ctx, targetIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config items: %w", err)
	}
	var scraperIDs []string
	for _, ci := range items {
		configMap[ci.ID] = ci
		if ci.ScraperID != nil && *ci.ScraperID != "" {
			scraperIDs = append(scraperIDs, *ci.ScraperID)
		}
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
				PageSize:  opts.pageSizeFor(opts.MaxChanges),
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get changes: %w", err)
		}
		detailsByID, err := loadCatalogChangeDetails(ctx, resp.Changes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load change details: %w", err)
		}
		entry.Changes = make([]api.CatalogReportChange, 0, len(resp.Changes))
		for _, c := range resp.Changes {
			name, typ := configMeta(c.ConfigID)
			r := newCatalogReportChangeFromRow(c, name, typ, detailsByID[c.ID])
			if err := mapper.Apply(&r); err != nil {
				return nil, nil, fmt.Errorf("failed to apply change mappings for %s: %w", c.ID, err)
			}
			entry.Changes = append(entry.Changes, r)
		}
		entry.ChangeCount = len(entry.Changes)

		if opts.ChangeArtifacts && len(entry.Changes) > 0 {
			attachChangeArtifacts(ctx, entry.Changes, opts.MaxItemArtifacts)
		}
	}

	if opts.Sections.Insights {
		resp, err := query.FindCatalogInsights(ctx, query.CatalogInsightsSearchRequest{
			BaseCatalogSearch: query.BaseCatalogSearch{
				CatalogID: catalogIDsCSV,
				PageSize:  opts.pageSizeFor(0),
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get insights: %w", err)
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
			return nil, nil, fmt.Errorf("failed to get access: %w", err)
		}
		if opts.ExpandGroups && hasGroupRow(rbacRows) {
			members, err := db.GetGroupMembersForConfigs(ctx, targetIDs)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get group members: %w", err)
			}
			rbacRows = ExpandGroupAccess(rbacRows, members)
		}
		entry.RBACResources = groupRBACByConfig(rbacRows, configMap, opts)
		for _, r := range entry.RBACResources {
			entry.AccessCount += len(r.Users)
		}
		entry.Access = make([]api.CatalogReportAccess, 0, len(rbacRows))
		for _, row := range rbacRows {
			entry.Access = append(entry.Access, rbacRowToAccess(row))
		}
	}

	if opts.Sections.AccessLogs {
		logs, err := getAccessLogs(ctx, targetIDs, sinceTime, opts.effectiveMax(0))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get access logs: %w", err)
		}
		entry.AccessLogs = lo.Map(logs, func(l accessLogRow, _ int) api.CatalogReportAccessLog {
			return newAccessLogEntry(l)
		})
	}

	if opts.Sections.Relationships && tree != nil {
		entry.RelationshipTree = configTreeNodeToReport(tree, opts.IncludedConfigIDs)
	}

	return entry, scraperIDs, nil
}

func newCatalogReportChangeFromRow(c query.ConfigChangeRow, configName, configType string, details map[string]any) api.CatalogReportChange {
	r := api.CatalogReportChange{
		ID:                c.ID,
		ConfigID:          c.ConfigID,
		ConfigName:        configName,
		ConfigType:        configType,
		Permalink:         api.ConfigPermalink(c.ConfigID),
		ChangeType:        c.ChangeType,
		Severity:          c.Severity,
		Source:            c.Source,
		Summary:           c.Summary,
		Details:           details,
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
}

func loadCatalogChangeDetails(ctx context.Context, rows []query.ConfigChangeRow) (map[string]map[string]any, error) {
	if len(rows) == 0 {
		return map[string]map[string]any{}, nil
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		id, err := uuid.Parse(row.ID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return map[string]map[string]any{}, nil
	}

	changes, err := query.GetCatalogChangesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	detailsByID := make(map[string]map[string]any, len(changes))
	for _, change := range changes {
		detailsByID[change.ID.String()] = decodeJSONMap(change.Details)
	}

	return detailsByID, nil
}

func decodeJSONMap(raw dutyTypes.JSON) map[string]any {
	if len(raw) == 0 {
		return nil
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}

	return decoded
}

// resolveParents derives report ancestry from config.Path to avoid recursive
// ParentID walks that can loop forever on cyclic catalog data.
func resolveParents(ctx context.Context, config *models.ConfigItem) []models.ConfigItem {
	parentIDs := parentIDsFromPath(config)
	if len(parentIDs) == 0 {
		return nil
	}

	loaded, err := query.GetConfigsByIDs(ctx, parentIDs)
	if err != nil || len(loaded) == 0 {
		return nil
	}

	byID := make(map[uuid.UUID]models.ConfigItem, len(loaded))
	for _, ci := range loaded {
		byID[ci.ID] = ci
	}

	parents := make([]models.ConfigItem, 0, len(parentIDs))
	for _, id := range parentIDs {
		if ci, ok := byID[id]; ok {
			parents = append(parents, ci)
		}
	}

	return parents
}

func parentIDsFromPath(config *models.ConfigItem) []uuid.UUID {
	if config == nil || config.Path == "" {
		return nil
	}

	segments := strings.Split(config.Path, ".")
	parentIDs := make([]uuid.UUID, 0, len(segments))
	for _, seg := range segments {
		id, err := uuid.Parse(seg)
		if err != nil || id == config.ID {
			continue
		}
		parentIDs = append(parentIDs, id)
	}

	return parentIDs
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

func getAccessLogs(ctx context.Context, configIDs []uuid.UUID, since time.Time, limit int) (results []accessLogRow, err error) {
	timer := query.NewQueryLogger(ctx).Start("AccessLogs").Arg("configIDs", len(configIDs))
	defer timer.End(&err)

	q := ctx.DB().
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
		Order("config_access_logs.created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err = q.Scan(&results).Error; err != nil {
		return nil, err
	}
	timer.Results(results)
	return results, nil
}

func rbacRowToAccess(r db.RBACAccessRow) api.CatalogReportAccess {
	a := api.CatalogReportAccess{
		ConfigID:        r.ConfigID.String(),
		ConfigName:      r.ConfigName,
		ConfigType:      r.ConfigType,
		Permalink:       api.ConfigPermalink(r.ConfigID.String()),
		UserID:          r.UserID.String(),
		UserName:        r.UserName,
		Email:           r.Email,
		Role:            r.Role,
		RoleExternalIDs: []string(r.RoleExternalIDs),
		UserType:        r.UserType,
		CreatedAt:       r.CreatedAt.Format(time.RFC3339),
	}
	if r.LastSignedInAt != nil {
		s := r.LastSignedInAt.Format(time.RFC3339)
		a.LastSignedInAt = &s
	}
	if r.LastReviewedAt != nil {
		s := r.LastReviewedAt.Format(time.RFC3339)
		a.LastReviewedAt = &s
	}
	return a
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

func attachChangeArtifacts(ctx context.Context, changes []api.CatalogReportChange, maxPerSource int) {
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
				Checksum:    a.Checksum,
				Path:        a.Path,
				CreatedAt:   a.CreatedAt.Format(time.RFC3339),
			}
			if isEmbeddableContentType(a.ContentType) {
				if data, err := a.GetContent(); err == nil && len(data) > 0 {
					ra.DataURI = fmt.Sprintf("data:%s;base64,%s", a.ContentType, base64.StdEncoding.EncodeToString(data))
				}
			}
			changes[i].Artifacts = append(changes[i].Artifacts, ra)
		}
	}

	capArtifactsPerSource(changes, maxPerSource)
}

// capArtifactsPerSource trims each change's Artifacts slice so that across the
// full []changes no more than maxPerSource artifacts survive per change.Source
// bucket. Changes are processed in order; artifacts for sources that have
// already reached the cap are dropped. A maxPerSource <= 0 means no cap.
func capArtifactsPerSource(changes []api.CatalogReportChange, maxPerSource int) {
	if maxPerSource <= 0 {
		return
	}
	counts := make(map[string]int)
	for i := range changes {
		if len(changes[i].Artifacts) == 0 {
			continue
		}
		src := changes[i].Source
		remaining := maxPerSource - counts[src]
		if remaining <= 0 {
			changes[i].Artifacts = nil
			continue
		}
		if len(changes[i].Artifacts) > remaining {
			changes[i].Artifacts = changes[i].Artifacts[:remaining]
		}
		counts[src] += len(changes[i].Artifacts)
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
	staleDays := opts.StaleDays()
	reviewDays := opts.ReviewOverdueDays()
	staleThreshold := time.Now().AddDate(0, 0, -staleDays)
	reviewThreshold := time.Now().AddDate(0, 0, -reviewDays)

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
			RoleExternalIDs: []string(row.RoleExternalIDs),
			RoleSource:      row.RoleSource(),
			SourceSystem:    row.UserType,
			CreatedAt:       row.CreatedAt,
			LastSignedInAt:  row.LastSignedInAt,
			LastReviewedAt:  row.LastReviewedAt,
			IsStale:         staleDays > 0 && (row.LastSignedInAt == nil || row.LastSignedInAt.Before(staleThreshold)),
			IsReviewOverdue: reviewDays > 0 && (row.LastReviewedAt == nil || row.LastReviewedAt.Before(reviewThreshold)),
		})
	}

	return lo.Map(order, func(id uuid.UUID, _ int) api.RBACResource {
		return *grouped[id]
	})
}

// classifyRootsAndBreadcrumbs marks each entry as a root or a child (relative
// to the selected set) and attaches a breadcrumb of selected ancestors
// root → parent for children. Used by groupBy=none to render roots first and
// children under a breadcrumb path instead of a per-entry relationship tree.
func classifyRootsAndBreadcrumbs(configs []models.ConfigItem, entries []api.CatalogReportEntry) {
	byID := make(map[uuid.UUID]*models.ConfigItem, len(configs))
	for i := range configs {
		byID[configs[i].ID] = &configs[i]
	}

	entryIDs := make(map[uuid.UUID]bool, len(entries))
	for _, e := range entries {
		if id, err := uuid.Parse(e.ConfigItem.ID); err == nil {
			entryIDs[id] = true
		}
	}

	for i := range entries {
		id, err := uuid.Parse(entries[i].ConfigItem.ID)
		if err != nil {
			entries[i].IsRoot = true
			continue
		}
		config, ok := byID[id]
		if !ok {
			entries[i].IsRoot = true
			continue
		}

		var crumbs []api.CatalogReportConfigItem
		for _, pid := range parentIDsFromPath(config) {
			if !entryIDs[pid] {
				continue
			}
			if parent, ok := byID[pid]; ok {
				crumbs = append(crumbs, api.NewCatalogReportConfigItem(*parent))
			}
		}
		if len(crumbs) == 0 {
			entries[i].IsRoot = true
			continue
		}
		entries[i].Breadcrumb = crumbs
	}
}

// buildRecursiveRelationshipTree assembles a single ancestry tree from a set
// of configs resolved via --recursive. Each config's Path is used to find its
// ancestors so descendants render under their real parent instead of as
// disconnected subtrees. Configs in `configs` are marked with edgeType="target"
// so the renderer can emphasize them. If entryTrees contains a per-entry
// relationship tree for a target config, its children are grafted onto that
// target node so the unified tree includes the related resources the per-entry
// build already loaded.
func buildRecursiveRelationshipTree(ctx context.Context, configs []models.ConfigItem, entryTrees map[uuid.UUID]*api.CatalogReportTreeNode) (*api.CatalogReportTreeNode, error) {
	if len(configs) == 0 {
		return nil, nil
	}

	rawByID := make(map[uuid.UUID]models.ConfigItem, len(configs))
	targets := make(map[uuid.UUID]bool, len(configs))
	for _, c := range configs {
		rawByID[c.ID] = c
		targets[c.ID] = true
	}

	missingSet := make(map[uuid.UUID]bool)
	for i := range configs {
		for _, pid := range parentIDsFromPath(&configs[i]) {
			if _, have := rawByID[pid]; !have {
				missingSet[pid] = true
			}
		}
	}
	if len(missingSet) > 0 {
		missing := make([]uuid.UUID, 0, len(missingSet))
		for id := range missingSet {
			missing = append(missing, id)
		}
		loaded, err := query.GetConfigsByIDs(ctx, missing)
		if err != nil {
			return nil, fmt.Errorf("failed to load ancestor configs: %w", err)
		}
		for _, c := range loaded {
			rawByID[c.ID] = c
		}
	}

	// Build an intermediate pointer-based tree so deep chains wire up
	// correctly regardless of iteration order, then convert to the
	// value-based report shape once all edges are known.
	type mutableNode struct {
		item     api.CatalogReportConfigItem
		edgeType string
		children []*mutableNode
	}

	nodes := make(map[uuid.UUID]*mutableNode, len(rawByID))
	for id, c := range rawByID {
		edge := "parent"
		if targets[id] {
			edge = "target"
		}
		nodes[id] = &mutableNode{
			item:     api.NewCatalogReportConfigItem(c),
			edgeType: edge,
		}
	}

	var roots []*mutableNode
	for id, c := range rawByID {
		parents := parentIDsFromPath(&c)
		attached := false
		for i := len(parents) - 1; i >= 0; i-- {
			if parent, ok := nodes[parents[i]]; ok {
				parent.children = append(parent.children, nodes[id])
				attached = true
				break
			}
		}
		if !attached {
			roots = append(roots, nodes[id])
		}
	}

	var toReport func(n *mutableNode) api.CatalogReportTreeNode
	toReport = func(n *mutableNode) api.CatalogReportTreeNode {
		out := api.CatalogReportTreeNode{
			CatalogReportConfigItem: n.item,
			EdgeType:                n.edgeType,
		}
		for _, c := range n.children {
			out.Children = append(out.Children, toReport(c))
		}
		// Graft related resources from the per-entry tree onto target nodes.
		// Skip children whose IDs are already present in the ancestry tree to
		// avoid duplicating nodes that were loaded as ancestors/descendants.
		if n.edgeType == "target" {
			if id, err := uuid.Parse(n.item.ID); err == nil {
				if entryTree, ok := entryTrees[id]; ok && entryTree != nil {
					existing := make(map[string]bool, len(out.Children))
					for _, c := range out.Children {
						if c.ID != "" {
							existing[c.ID] = true
						}
					}
					for _, c := range entryTree.Children {
						if c.ID != "" && existing[c.ID] {
							continue
						}
						out.Children = append(out.Children, c)
					}
				}
			}
		}
		return out
	}

	switch len(roots) {
	case 0:
		return nil, nil
	case 1:
		out := toReport(roots[0])
		return &out, nil
	}

	virtual := &api.CatalogReportTreeNode{
		CatalogReportConfigItem: api.CatalogReportConfigItem{
			Name: fmt.Sprintf("%d configs", len(configs)),
		},
	}
	for _, r := range roots {
		virtual.Children = append(virtual.Children, toReport(r))
	}
	return virtual, nil
}

func configTreeNodeToReport(n *query.ConfigTreeNode, include map[uuid.UUID]bool) *api.CatalogReportTreeNode {
	return buildReportTreeNode(n, include, make(map[uuid.UUID]bool))
}

func buildReportTreeNode(n *query.ConfigTreeNode, include map[uuid.UUID]bool, visited map[uuid.UUID]bool) *api.CatalogReportTreeNode {
	result := &api.CatalogReportTreeNode{
		CatalogReportConfigItem: api.NewCatalogReportConfigItem(n.ConfigItem),
		EdgeType:                n.EdgeType,
		Relation:                n.Relation,
	}
	if visited[n.ID] {
		return result
	}
	visited[n.ID] = true
	for _, c := range n.Children {
		child := buildReportTreeNode(c, include, visited)
		if child == nil {
			continue
		}
		result.Children = append(result.Children, *child)
	}
	if len(include) > 0 && !include[n.ID] && len(result.Children) == 0 {
		return nil
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
