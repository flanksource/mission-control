package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/duration"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/pkg/clients/git"
	"github.com/flanksource/incident-commander/pkg/clients/git/connectors"
	"github.com/flanksource/incident-commander/report"
	"github.com/flanksource/incident-commander/report/catalog"
	"github.com/flanksource/incident-commander/views"
)

// defaultCatalogEntryFile is the embedded TSX template used when no file is given.
const defaultCatalogEntryFile = "CatalogReport.tsx"

type ReportResult struct {
	Format    string               `json:"format,omitempty"`
	Logs      string               `json:"logs,omitempty"`
	Artifacts []artifacts.Artifact `json:"-"`
}

func (r *ReportResult) GetArtifacts() []artifacts.Artifact { return r.Artifacts }

type Report struct {
	// ActionID identifies the playbook run action row that receives progress
	// logs while the report builds. uuid.Nil disables streaming.
	ActionID uuid.UUID

	logs             []string
	lastLogPersisted time.Time
	logsDirty        bool
}

const reportLogPersistInterval = time.Second

// logf records a progress line and streams the accumulated log to the run
// action row so the UI can show it while the action is still running.
func (r *Report) logf(ctx context.Context, format string, args ...any) {
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	r.logs = append(r.logs, line)
	r.logsDirty = true

	if time.Since(r.lastLogPersisted) >= reportLogPersistInterval {
		r.persistLogs(ctx)
	}
}

func (r *Report) persistLogs(ctx context.Context) {
	if !r.logsDirty || r.ActionID == uuid.Nil || ctx.DB() == nil {
		return
	}

	runAction := models.PlaybookRunAction{ID: r.ActionID}
	if err := runAction.Update(ctx.DB(), map[string]any{"result": types.JSONMap{"logs": r.logText()}}); err != nil {
		ctx.Logger.V(3).Infof("failed to stream report progress: %v", err)
		return
	}
	r.lastLogPersisted = time.Now()
	r.logsDirty = false
}

func (r *Report) logText() string { return strings.Join(r.logs, "\n") }

func (r *Report) Run(ctx context.Context, action v1.ReportAction) (*ReportResult, error) {
	return r.runWithConfigs(ctx, action, nil)
}

// RunWithConfigs executes a report using config items already resolved from a
// configs playbook parameter.
func (r *Report) RunWithConfigs(ctx context.Context, action v1.ReportAction, configs []models.ConfigItem) (*ReportResult, error) {
	return r.runWithConfigs(ctx, action, configs)
}

func (r *Report) runWithConfigs(ctx context.Context, action v1.ReportAction, configs []models.ConfigItem) (*ReportResult, error) {
	result, err := r.run(ctx, action, configs)
	if result == nil {
		result = &ReportResult{}
	}
	result.Logs = r.logText()
	r.persistLogs(ctx)
	return result, err
}

func (r *Report) run(ctx context.Context, action v1.ReportAction, configs []models.ConfigItem) (*ReportResult, error) {
	format := action.Format
	if format == "" {
		format = "json"
	}

	if action.Configs != nil && action.ConfigsFromParams {
		return nil, fmt.Errorf("configs and configsFromParams are mutually exclusive")
	}
	if action.View != "" {
		if action.ConfigsFromParams {
			return nil, fmt.Errorf("view and configsFromParams are mutually exclusive")
		}
		return r.runView(ctx, action, format)
	}

	if action.ConfigsFromParams {
		if configs == nil {
			return nil, fmt.Errorf("configsFromParams requires a resolved configs parameter named \"configs\"")
		}
	} else if action.Configs == nil {
		return nil, fmt.Errorf("either view, configs, or configsFromParams must be specified")
	}

	return r.runCatalog(ctx, action, format, configs)
}

func (r *Report) runView(ctx context.Context, action v1.ReportAction, format string) (*ReportResult, error) {
	namespace, name := parseNamespacedName(ctx, action.View)
	v, err := db.GetView(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get view %s: %w", action.View, err)
	}
	if v == nil {
		return nil, fmt.Errorf("view %s not found", action.View)
	}

	r.logf(ctx, "exporting view %s as %s", action.View, format)
	rendered, err := views.Export(ctx, v, action.Variables, format, action.Facet)
	if err != nil {
		return nil, fmt.Errorf("failed to export view: %w", err)
	}
	r.logf(ctx, "view exported (%d bytes)", len(rendered))

	return reportResult(format, rendered), nil
}

func (r *Report) runCatalog(ctx context.Context, action v1.ReportAction, format string, configs []models.ConfigItem) (*ReportResult, error) {
	opts, err := catalogOptions(action)
	if err != nil {
		return nil, err
	}
	opts.Progress = func(format string, args ...any) { r.logf(ctx, format, args...) }

	if configs == nil {
		r.logf(ctx, "resolving config items")
		configs, err = query.FindConfigsByResourceSelector(ctx, -1, *action.Configs)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve configs: %w", err)
		}
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("no config items matched the selector")
	}
	r.logf(ctx, "resolved %d root config item(s)", len(configs))

	selection, err := catalog.ResolveSelection(ctx, configs, opts.Recursive, opts.Settings.FilterQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve report selection: %w", err)
	}
	r.logf(ctx, "selected %d root(s), %d total config item(s)", len(selection.Roots), len(selection.Items))

	data, err := catalog.BuildSelection(ctx, selection, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build catalog report: %w", err)
	}
	r.logf(ctx, "report data ready: %d entries, %d changes, %d insights", len(data.Entries), len(data.Changes), len(data.Analyses))

	rendered, err := r.renderCatalog(ctx, action, data, format)
	if err != nil {
		return nil, err
	}
	r.logf(ctx, "report rendered as %s (%d bytes)", format, len(rendered))

	return reportResult(format, rendered), nil
}

func (r *Report) renderCatalog(ctx context.Context, action v1.ReportAction, data api.CatalogReport, format string) ([]byte, error) {
	switch format {
	case "html", "facet-html":
		return r.renderCatalogFacet(ctx, action, data, "html")
	case "pdf", "facet-pdf":
		return r.renderCatalogFacet(ctx, action, data, "pdf")
	default:
		return json.MarshalIndent(data, "", "  ")
	}
}

func (r *Report) renderCatalogFacet(ctx context.Context, action v1.ReportAction, data api.CatalogReport, facetFormat string) ([]byte, error) {
	srcDir, entryFile, err := resolveReportSource(ctx, action.File)
	if err != nil {
		return nil, err
	}

	server, err := report.ResolveServer(ctx, action.Facet)
	if err != nil {
		return nil, err
	}

	if server.Configured() {
		r.logf(ctx, "rendering %s via facet service %s", facetFormat, server.BaseURL)
	} else {
		r.logf(ctx, "rendering %s via local facet binary", facetFormat)
	}

	result, err := report.RenderWith(ctx, data, facetFormat, entryFile, srcDir, server)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// catalogOptions builds the catalog report options from the action. When no
// sections are specified, the defaults match a bare `catalog report` run.
func catalogOptions(action v1.ReportAction) (catalog.Options, error) {
	if action.GroupBy != "" && action.GroupBy != "none" && action.GroupBy != "merged" && action.GroupBy != "config" {
		return catalog.Options{}, fmt.Errorf("invalid groupBy %q: expected none, merged, or config", action.GroupBy)
	}
	recursive, err := reportBool(action.Recursive, false, "recursive")
	if err != nil {
		return catalog.Options{}, err
	}

	changeArtifacts, err := reportBool(action.ChangeArtifacts, false, "changeArtifacts")
	if err != nil {
		return catalog.Options{}, err
	}
	expandGroups, err := reportBool(action.ExpandGroups, false, "expandGroups")
	if err != nil {
		return catalog.Options{}, err
	}
	audit, err := reportBool(action.Audit, false, "audit")
	if err != nil {
		return catalog.Options{}, err
	}
	settings, settingsSource, err := catalog.ResolveSettings("")
	if err != nil {
		return catalog.Options{}, fmt.Errorf("load report settings: %w", err)
	}
	for _, filter := range action.Filters {
		for _, item := range strings.Split(filter, ",") {
			if item = strings.TrimSpace(item); item != "" {
				settings.Filters = append(settings.Filters, item)
			}
		}
	}

	opts := catalog.Options{
		Title:           action.Title,
		Recursive:       recursive,
		GroupBy:         action.GroupBy,
		ChangeArtifacts: changeArtifacts,
		ExpandGroups:    expandGroups,
		Audit:           audit,
		Settings:        settings,
		SettingsPath:    settingsSource,
	}

	if action.Sections != nil {
		sections, err := reportSections(*action.Sections)
		if err != nil {
			return catalog.Options{}, err
		}
		opts.Sections = sections
	} else {
		opts.Sections = api.CatalogReportSections{
			Changes:       true,
			Insights:      true,
			Relationships: true,
			Access:        true,
		}
	}

	if action.Since != "" {
		d, err := duration.ParseDuration(action.Since)
		if err != nil {
			return catalog.Options{}, fmt.Errorf("invalid since %q: %w", action.Since, err)
		}
		opts.Since = time.Duration(d)
	}

	return opts, nil
}

func reportSections(sections v1.ReportSections) (api.CatalogReportSections, error) {
	resolved := api.CatalogReportSections{}
	fields := []struct {
		name         string
		value        v1.TemplatedBool
		defaultValue bool
		target       *bool
	}{
		{name: "sections.changes", value: sections.Changes, defaultValue: true, target: &resolved.Changes},
		{name: "sections.insights", value: sections.Insights, defaultValue: true, target: &resolved.Insights},
		{name: "sections.relationships", value: sections.Relationships, defaultValue: true, target: &resolved.Relationships},
		{name: "sections.access", value: sections.Access, defaultValue: true, target: &resolved.Access},
		{name: "sections.accessLogs", value: sections.AccessLogs, target: &resolved.AccessLogs},
		{name: "sections.configJSON", value: sections.ConfigJSON, target: &resolved.ConfigJSON},
		{name: "sections.resolvedInsights", value: sections.ResolvedInsights, target: &resolved.ResolvedInsights},
	}

	for _, field := range fields {
		value, err := reportBool(field.value, field.defaultValue, field.name)
		if err != nil {
			return api.CatalogReportSections{}, err
		}
		*field.target = value
	}
	return resolved, nil
}

func reportBool(value v1.TemplatedBool, defaultValue bool, field string) (bool, error) {
	resolved, err := value.Resolve(defaultValue)
	if err != nil {
		return false, fmt.Errorf("%s: %w", field, err)
	}
	return resolved, nil
}

// resolveReportSource resolves the TSX template source directory and entry file.
// When file is nil, the embedded CatalogReport.tsx is used. A local path is
// resolved against the working directory unless absolute, and its directory
// must contain the report scaffold needed to compile the entry file. A git
// source is cloned and the clone root becomes the source directory, so the
// entry file may live in a subdirectory and import from anywhere in the repo.
func resolveReportSource(ctx context.Context, file *v1.ReportFile) (srcDir, entryFile string, err error) {
	if file == nil {
		return "", defaultCatalogEntryFile, nil
	}

	if file.Git != nil {
		return resolveGitReportSource(ctx, file.Git)
	}

	if file.Path == "" {
		return "", "", fmt.Errorf("report file requires either path or git")
	}

	path := file.Path
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("resolve working directory: %w", err)
		}
		path = filepath.Join(cwd, path)
	}

	if _, err := os.Stat(path); err != nil {
		return "", "", fmt.Errorf("report file %s: %w", path, err)
	}

	return filepath.Dir(path), filepath.Base(path), nil
}

func resolveGitReportSource(ctx context.Context, src *v1.ReportGitFile) (srcDir, entryFile string, err error) {
	base := src.Base
	if base == "" {
		base = "main"
	}

	spec := &connectors.GitopsAPISpec{
		Repository: src.URL,
		Base:       base,
		Branch:     base,
	}

	if src.Connection != "" {
		conn, err := pkgConnection.Get(ctx, src.Connection)
		if err != nil {
			return "", "", ctx.Oops().Wrap(err)
		}
		if conn == nil {
			return "", "", fmt.Errorf("connection %s not found", src.Connection)
		}
		if err := applyGitConnection(ctx, spec, conn); err != nil {
			return "", "", err
		}
	}

	_, work, err := git.Clone(ctx, spec)
	if err != nil {
		return "", "", fmt.Errorf("failed to clone %s: %w", src.URL, err)
	}

	full := filepath.Join(work.Filesystem.Root(), src.File)
	if _, err := os.Stat(full); err != nil {
		return "", "", fmt.Errorf("file %s not found in repo %s: %w", src.File, src.URL, err)
	}

	return work.Filesystem.Root(), src.File, nil
}

func reportResult(format string, rendered []byte) *ReportResult {
	return &ReportResult{
		Format: format,
		Artifacts: []artifacts.Artifact{{
			ContentType: formatContentType(format),
			Content:     io.NopCloser(bytes.NewReader(rendered)),
			Path:        "report" + formatExtension(format),
		}},
	}
}

func parseNamespacedName(ctx context.Context, ref string) (string, string) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return ctx.GetNamespace(), ref
}

// formatMeta maps known report formats to their MIME content type and file extension.
// When adding new binary export formats (e.g. Excel, images), add an entry here
// to keep formatContentType and formatExtension in sync.
var formatMeta = map[string]struct {
	contentType string
	extension   string
}{
	"pdf":        {"application/pdf", ".pdf"},
	"facet-pdf":  {"application/pdf", ".pdf"},
	"html":       {"text/html", ".html"},
	"facet-html": {"text/html", ".html"},
	"csv":        {"text/csv", ".csv"},
}

func formatContentType(format string) string {
	if meta, ok := formatMeta[format]; ok {
		return meta.contentType
	}
	return "application/json"
}

func formatExtension(format string) string {
	if meta, ok := formatMeta[format]; ok {
		return meta.extension
	}
	return ".json"
}
