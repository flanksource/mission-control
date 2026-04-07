package catalog_report

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/scraper_report"
)

type ExportResult struct {
	Data     []byte
	SrcDir   string
	Entry    string
	DataFile string
	Settings string
}

func Export(ctx context.Context, configs []models.ConfigItem, opts Options, format string) (*ExportResult, error) {
	var queryLog *query.QueryLog
	if opts.Audit {
		ctx, queryLog = query.WithQueryLog(ctx)
	}

	r, scraperIDs, err := BuildReport(ctx, configs, opts)
	if err != nil {
		return nil, err
	}

	ctx.Logger.V(3).Infof("Report built: %d entries, %d changes, %d analyses",
		len(r.Entries), len(r.Changes), len(r.Analyses))

	if opts.Audit {
		r.Audit = buildAudit(ctx, opts, scraperIDs, queryLog)
	}

	result := &ExportResult{}
	if opts.Settings != nil {
		result.Settings = opts.SettingsPath
	}

	switch format {
	case "html", "facet-html":
		result.Data, result.SrcDir, result.Entry, result.DataFile, err = renderFacetResult(ctx, r, "html")
	case "pdf", "facet-pdf":
		result.Data, result.SrcDir, result.Entry, result.DataFile, err = renderFacetResult(ctx, r, "pdf")
	default:
		result.Data, err = json.MarshalIndent(r, "", "  ")
	}

	return result, err
}

func buildAudit(ctx context.Context, opts Options, scraperIDs []string, queryLog *query.QueryLog) *api.CatalogReportAudit {
	audit := &api.CatalogReportAudit{
		BuildCommit:  api.BuildCommit,
		BuildVersion: api.BuildVersion,
		Options: api.CatalogReportOptions{
			Title:           opts.Title,
			Since:           opts.Since.String(),
			Sections:        opts.Sections,
			Recursive:       opts.Recursive,
			GroupBy:         opts.GroupBy,
			ChangeArtifacts: opts.ChangeArtifacts,
		},
		Scrapers: []api.ScraperInfo{},
		Queries:  []api.CatalogReportQuery{},
	}

	if opts.Settings != nil {
		audit.Options.Filters = opts.Settings.Filters
		if opts.Settings.Thresholds.StaleDays > 0 || opts.Settings.Thresholds.ReviewOverdueDays > 0 {
			audit.Options.Thresholds = &api.CatalogReportThresholds{
				StaleDays:         opts.StaleDays(),
				ReviewOverdueDays: opts.ReviewOverdueDays(),
			}
		}
		audit.Options.CategoryMappings = opts.Settings.CategoryMappings
	}

	audit.GitStatus = gitStatus()

	if queryLog != nil {
		for _, e := range queryLog.Entries() {
			audit.Queries = append(audit.Queries, api.CatalogReportQuery{
				Name:     e.Name,
				Args:     e.Args,
				Count:    e.Count,
				Duration: e.Duration,
				Error:    e.Error,
				Summary:  e.Summary,
				Pretty:   e.Pretty,
			})
		}
	}

	sort.Strings(scraperIDs)
	for _, sid := range scraperIDs {
		id, err := uuid.Parse(sid)
		if err != nil {
			continue
		}
		info, err := scraper_report.BuildScraperInfo(ctx, id)
		if err != nil {
			ctx.Logger.V(2).Infof("failed to build scraper info for %s: %v", sid, err)
			continue
		}
		audit.Scrapers = append(audit.Scrapers, *info)
	}

	return audit
}

func gitStatus() string {
	out, err := exec.Command("git", "status", "--short").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func initSlices(r *api.CatalogReport) api.CatalogReport {
	out := *r
	if out.Entries == nil {
		out.Entries = []api.CatalogReportEntry{}
	}
	for i := range out.Entries {
		if out.Entries[i].Parents == nil {
			out.Entries[i].Parents = []api.CatalogReportConfigItem{}
		}
		if out.Entries[i].Changes == nil {
			out.Entries[i].Changes = []api.CatalogReportChange{}
		}
		if out.Entries[i].Analyses == nil {
			out.Entries[i].Analyses = []api.CatalogReportAnalysis{}
		}
		if out.Entries[i].Access == nil {
			out.Entries[i].Access = []api.CatalogReportAccess{}
		}
		if out.Entries[i].AccessLogs == nil {
			out.Entries[i].AccessLogs = []api.CatalogReportAccessLog{}
		}
	}
	if out.Parents == nil {
		out.Parents = []models.ConfigItem{}
	}
	if out.Changes == nil {
		out.Changes = []api.CatalogReportChange{}
	}
	if out.Analyses == nil {
		out.Analyses = []api.CatalogReportAnalysis{}
	}
	if out.Relationships == nil {
		out.Relationships = []api.CatalogReportRelationship{}
	}
	if out.RelatedConfigs == nil {
		out.RelatedConfigs = []api.CatalogReportConfigItem{}
	}
	if out.Access == nil {
		out.Access = []api.CatalogReportAccess{}
	}
	if out.AccessLogs == nil {
		out.AccessLogs = []api.CatalogReportAccessLog{}
	}
	if out.ConfigGroups == nil {
		out.ConfigGroups = []api.CatalogReportConfigGroup{}
	}
	for i := range out.ConfigGroups {
		if out.ConfigGroups[i].Changes == nil {
			out.ConfigGroups[i].Changes = []api.CatalogReportChange{}
		}
		if out.ConfigGroups[i].Analyses == nil {
			out.ConfigGroups[i].Analyses = []api.CatalogReportAnalysis{}
		}
		if out.ConfigGroups[i].Access == nil {
			out.ConfigGroups[i].Access = []api.CatalogReportAccess{}
		}
		if out.ConfigGroups[i].AccessLogs == nil {
			out.ConfigGroups[i].AccessLogs = []api.CatalogReportAccessLog{}
		}
	}
	if out.Audit != nil {
		if out.Audit.Scrapers == nil {
			out.Audit.Scrapers = []api.ScraperInfo{}
		}
		if out.Audit.Queries == nil {
			out.Audit.Queries = []api.CatalogReportQuery{}
		}
	}
	return out
}
