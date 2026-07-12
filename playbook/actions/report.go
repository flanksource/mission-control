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
	result, err := r.run(ctx, action)
	if result == nil {
		result = &ReportResult{}
	}
	result.Logs = r.logText()
	r.persistLogs(ctx)
	return result, err
}

func (r *Report) run(ctx context.Context, action v1.ReportAction) (*ReportResult, error) {
	format := action.Format
	if format == "" {
		format = "json"
	}

	if action.View != "" {
		return r.runView(ctx, action, format)
	}

	if action.Configs == nil {
		return nil, fmt.Errorf("either view or configs must be specified")
	}

	return r.runCatalog(ctx, action, format)
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

func (r *Report) runCatalog(ctx context.Context, action v1.ReportAction, format string) (*ReportResult, error) {
	r.logf(ctx, "resolving config items")
	configs, err := query.FindConfigsByResourceSelector(ctx, -1, *action.Configs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve configs: %w", err)
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("no config items matched the selector")
	}
	r.logf(ctx, "resolved %d config item(s)", len(configs))

	opts, err := catalogOptions(action)
	if err != nil {
		return nil, err
	}
	opts.Progress = func(format string, args ...any) { r.logf(ctx, format, args...) }

	data, err := catalog.Build(ctx, configs, opts)
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

	baseURL, token, timestampURL, err := views.ResolveFacetConnection(ctx, action.Facet)
	if err != nil {
		return nil, err
	}

	if baseURL != "" {
		r.logf(ctx, "rendering %s via facet service %s", facetFormat, baseURL)
		opts := report.RenderHTTPOptions{TimestampURL: timestampURL}
		// The embedded default ships the pristine embedded files; a custom file
		// ships its own directory.
		if action.File == nil {
			return report.RenderHTTP(ctx, baseURL, token, data, facetFormat, entryFile, opts)
		}
		return report.RenderHTTPFromDir(ctx, baseURL, token, data, facetFormat, srcDir, entryFile, opts)
	}

	r.logf(ctx, "rendering %s via local facet CLI", facetFormat)
	result, err := report.RenderCLIFromDir(data, facetFormat, srcDir, entryFile)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// catalogOptions builds the catalog report options from the action. When no
// sections are specified, the defaults match a bare `catalog report` run.
func catalogOptions(action v1.ReportAction) (catalog.Options, error) {
	opts := catalog.Options{
		Recursive: action.Recursive,
		GroupBy:   action.GroupBy,
	}

	if action.Sections != nil {
		opts.Sections = *action.Sections
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

// resolveReportSource resolves the TSX template source directory and entry file.
// When file is nil, the embedded CatalogReport.tsx is used. A local path is
// resolved against the working directory unless absolute, and its directory
// must contain the report scaffold needed to compile the entry file. A git
// source is cloned and the clone root becomes the source directory, so the
// entry file may live in a subdirectory and import from anywhere in the repo.
func resolveReportSource(ctx context.Context, file *v1.ReportFile) (srcDir, entryFile string, err error) {
	if file == nil {
		dir, err := report.SrcDir()
		if err != nil {
			return "", "", fmt.Errorf("prepare report src dir: %w", err)
		}
		return dir, defaultCatalogEntryFile, nil
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
