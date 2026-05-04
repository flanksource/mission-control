package catalog

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	reportAPI "github.com/flanksource/incident-commander/api"
	reportCatalog "github.com/flanksource/incident-commander/report/catalog"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type CatalogReportRoot struct {
	ID              string `json:"id"`
	IncludeChildren bool   `json:"includeChildren"`
}

type CatalogReportRequest struct {
	Format      string              `json:"format"`
	Roots       []CatalogReportRoot `json:"roots"`
	SelectedIDs []string            `json:"selectedIds"`

	Title             string   `json:"title"`
	Since             string   `json:"since"`
	Recursive         bool     `json:"recursive"`
	GroupBy           string   `json:"groupBy"`
	ChangeArtifacts   bool     `json:"changeArtifacts"`
	Audit             bool     `json:"audit"`
	ExpandGroups      bool     `json:"expandGroups"`
	Limit             int      `json:"limit"`
	MaxItems          int      `json:"maxItems"`
	MaxChanges        int      `json:"maxChanges"`
	MaxItemArtifacts  int      `json:"maxItemArtifacts"`
	StaleDays         int      `json:"staleDays"`
	ReviewOverdueDays int      `json:"reviewOverdueDays"`
	Filters           []string `json:"filters"`
	Changes           *bool    `json:"changes"`
	Insights          *bool    `json:"insights"`
	Relationships     *bool    `json:"relationships"`
	Access            *bool    `json:"access"`
	AccessLogs        *bool    `json:"accessLogs"`
	ConfigJSON        *bool    `json:"configJSON"`
}

type CatalogReportPreviewResponse struct {
	Roots       []*query.ConfigTreeNode `json:"roots"`
	SelectedIDs []string                `json:"selectedIds"`
	Count       int                     `json:"count"`
}

func PreviewCatalogReport(c echo.Context) error {
	var req CatalogReportRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request: %v", err))
	}

	ctx := c.Request().Context().(context.Context)
	configs, err := resolveCatalogReportConfigs(ctx, req, true)
	if err != nil {
		return api.WriteError(c, err)
	}

	roots := buildConfigForest(configs)
	selectedIDs := flattenConfigTreeIDs(roots)

	return c.JSON(http.StatusOK, CatalogReportPreviewResponse{
		Roots:       roots,
		SelectedIDs: selectedIDs,
		Count:       len(selectedIDs),
	})
}

func GenerateCatalogReport(c echo.Context) error {
	var req CatalogReportRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request: %v", err))
	}

	format, contentType, extension, err := normalizeCatalogReportFormat(req.Format)
	if err != nil {
		return api.WriteError(c, err)
	}

	ctx := c.Request().Context().(context.Context)
	configs, err := resolveCatalogReportConfigs(ctx, req, false)
	if err != nil {
		return api.WriteError(c, err)
	}

	opts, err := catalogReportOptionsFromRequest(req)
	if err != nil {
		return api.WriteError(c, err)
	}
	opts.IncludedConfigIDs = includedConfigIDSet(configs)

	result, err := reportCatalog.Export(ctx, configs, opts, format)
	if err != nil {
		return api.WriteError(c, ctx.Oops().Wrapf(err, "failed to render catalog report"))
	}

	filename := catalogReportFilename(req.Title, extension)
	c.Response().Header().Set(echo.HeaderContentDisposition, mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	return c.Blob(http.StatusOK, contentType, result.Data)
}

func catalogReportOptionsFromRequest(req CatalogReportRequest) (reportCatalog.Options, error) {
	settings, settingsSource, err := reportCatalog.ResolveSettings("")
	if err != nil {
		return reportCatalog.Options{}, api.Errorf(api.EINTERNAL, "failed to load report settings: %v", err)
	}

	if req.StaleDays > 0 {
		settings.Thresholds.StaleDays = req.StaleDays
	}
	if req.ReviewOverdueDays > 0 {
		settings.Thresholds.ReviewOverdueDays = req.ReviewOverdueDays
	}
	if len(req.Filters) > 0 {
		settings.Filters = append(settings.Filters, req.Filters...)
	}

	opts := reportCatalog.Options{
		Title:            req.Title,
		Recursive:        req.Recursive,
		GroupBy:          req.GroupBy,
		ChangeArtifacts:  req.ChangeArtifacts,
		Audit:            req.Audit,
		ExpandGroups:     req.ExpandGroups,
		Limit:            req.Limit,
		MaxItems:         req.MaxItems,
		MaxChanges:       req.MaxChanges,
		MaxItemArtifacts: req.MaxItemArtifacts,
		Settings:         settings,
		SettingsPath:     settingsSource,
		Sections: reportAPI.CatalogReportSections{
			Changes:       boolOrDefault(req.Changes, true),
			Insights:      boolOrDefault(req.Insights, true),
			Relationships: boolOrDefault(req.Relationships, true),
			Access:        boolOrDefault(req.Access, true),
			AccessLogs:    boolOrDefault(req.AccessLogs, false),
			ConfigJSON:    boolOrDefault(req.ConfigJSON, false),
		},
	}

	if req.Since != "" {
		d, err := duration.ParseDuration(req.Since)
		if err != nil {
			return reportCatalog.Options{}, api.Errorf(api.EINVALID, "invalid since: %v", err)
		}
		opts.Since = time.Duration(d)
	}

	return opts, nil
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func resolveCatalogReportConfigs(ctx context.Context, req CatalogReportRequest, allowEmpty bool) ([]models.ConfigItem, error) {
	ids := make([]uuid.UUID, 0, len(req.SelectedIDs)+len(req.Roots))
	seen := map[uuid.UUID]bool{}
	add := func(id uuid.UUID) {
		if seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	for _, raw := range req.SelectedIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "invalid selected config id: %s", raw)
		}
		add(id)
	}

	for _, root := range req.Roots {
		id, err := uuid.Parse(root.ID)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "invalid root config id: %s", root.ID)
		}
		if !root.IncludeChildren {
			add(id)
			continue
		}
		tree, err := query.ConfigTree(ctx, id, query.ConfigTreeOptions{})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, ctx.Oops().Wrapf(err, "failed to expand config children for %s", id)
		}
		if tree == nil {
			continue
		}
		for _, childID := range tree.OutgoingIDs() {
			add(childID)
		}
	}

	if len(ids) == 0 {
		if allowEmpty {
			return nil, nil
		}
		return nil, api.Errorf(api.EINVALID, "select at least one config item")
	}

	loaded, err := getExistingCatalogReportConfigs(ctx, ids)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	byID := make(map[uuid.UUID]models.ConfigItem, len(loaded))
	for _, config := range loaded {
		byID[config.ID] = config
	}

	configs := make([]models.ConfigItem, 0, len(ids))
	for _, id := range ids {
		config, ok := byID[id]
		if !ok {
			continue
		}
		configs = append(configs, config)
	}

	if len(configs) == 0 && !allowEmpty {
		return nil, api.Errorf(api.EINVALID, "select at least one config item")
	}

	return configs, nil
}

func getExistingCatalogReportConfigs(ctx context.Context, ids []uuid.UUID) ([]models.ConfigItem, error) {
	configs := make([]models.ConfigItem, 0, len(ids))
	for _, id := range ids {
		config, err := query.ConfigItemFromCache(ctx, id.String())
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

func includedConfigIDSet(configs []models.ConfigItem) map[uuid.UUID]bool {
	ids := make(map[uuid.UUID]bool, len(configs))
	for _, config := range configs {
		ids[config.ID] = true
	}
	return ids
}

func buildConfigForest(configs []models.ConfigItem) []*query.ConfigTreeNode {
	nodes := make(map[uuid.UUID]*query.ConfigTreeNode, len(configs))
	for _, config := range configs {
		nodes[config.ID] = &query.ConfigTreeNode{
			ConfigItem: config,
			EdgeType:   "target",
		}
	}

	childIDs := map[uuid.UUID]bool{}
	for _, config := range configs {
		node := nodes[config.ID]
		parent := findSelectedConfigParent(config, nodes)
		if parent == nil {
			continue
		}
		node.EdgeType = "child"
		parent.Children = append(parent.Children, node)
		childIDs[config.ID] = true
	}

	roots := make([]*query.ConfigTreeNode, 0, len(configs))
	for _, config := range configs {
		if childIDs[config.ID] {
			continue
		}
		roots = append(roots, nodes[config.ID])
	}

	sortConfigTreeNodes(roots)
	return roots
}

func findSelectedConfigParent(config models.ConfigItem, nodes map[uuid.UUID]*query.ConfigTreeNode) *query.ConfigTreeNode {
	if config.ParentID != nil {
		if parent := nodes[*config.ParentID]; parent != nil && parent.ID != config.ID {
			return parent
		}
	}

	if config.Path == "" {
		return nil
	}
	segments := strings.Split(config.Path, ".")
	for i := len(segments) - 1; i >= 0; i-- {
		id, err := uuid.Parse(segments[i])
		if err != nil || id == config.ID {
			continue
		}
		if parent := nodes[id]; parent != nil {
			return parent
		}
	}
	return nil
}

func sortConfigTreeNodes(nodes []*query.ConfigTreeNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		left := fmt.Sprintf("%s/%s/%s", nodes[i].GetType(), nodes[i].GetName(), nodes[i].ID)
		right := fmt.Sprintf("%s/%s/%s", nodes[j].GetType(), nodes[j].GetName(), nodes[j].ID)
		return left < right
	})
	for _, node := range nodes {
		sortConfigTreeNodes(node.Children)
	}
}

func flattenConfigTreeIDs(nodes []*query.ConfigTreeNode) []string {
	ids := make([]string, 0)
	var walk func(nodes []*query.ConfigTreeNode)
	walk = func(nodes []*query.ConfigTreeNode) {
		for _, node := range nodes {
			ids = append(ids, node.ID.String())
			walk(node.Children)
		}
	}
	walk(nodes)
	return ids
}

func normalizeCatalogReportFormat(format string) (string, string, string, error) {
	switch format {
	case "", "pdf", "facet-pdf":
		return "facet-pdf", "application/pdf", "pdf", nil
	case "html", "facet-html":
		return "facet-html", "text/html; charset=utf-8", "html", nil
	case "json":
		return "json", "application/json", "json", nil
	default:
		return "", "", "", api.Errorf(api.EINVALID, "invalid format %q", format)
	}
}

var filenameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func catalogReportFilename(title string, extension string) string {
	base := strings.TrimSpace(title)
	if base == "" {
		base = "catalog-report"
	}
	base = strings.Trim(filenameUnsafe.ReplaceAllString(base, "-"), "-._")
	if base == "" {
		base = "catalog-report"
	}
	return fmt.Sprintf("%s.%s", strings.ToLower(base), extension)
}
