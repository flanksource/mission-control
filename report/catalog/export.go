package catalog

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/report/scraper"
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
		r.Audit = buildAudit(ctx, opts, configs, scraperIDs, queryLog)
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

func buildAudit(ctx context.Context, opts Options, configs []models.ConfigItem, scraperIDs []string, queryLog *query.QueryLog) *api.CatalogReportAudit {
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
		Groups:   []api.CatalogReportGroup{},
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
		info, err := scraper.BuildScraperInfo(ctx, id)
		if err != nil {
			ctx.Logger.V(2).Infof("failed to build scraper info for %s: %v", sid, err)
			continue
		}
		audit.Scrapers = append(audit.Scrapers, *info)
	}

	audit.Groups = buildAuditGroups(ctx, configs)

	return audit
}

func buildAuditGroups(ctx context.Context, configs []models.ConfigItem) []api.CatalogReportGroup {
	if len(configs) == 0 {
		return []api.CatalogReportGroup{}
	}

	configIDs := make([]uuid.UUID, 0, len(configs))
	for _, c := range configs {
		configIDs = append(configIDs, c.ID)
	}

	rows, err := db.GetGroupMembersForConfigs(ctx, configIDs)
	if err != nil {
		ctx.Logger.V(2).Infof("failed to load group members for audit: %v", err)
		return []api.CatalogReportGroup{}
	}

	// Preserve the SQL ORDER BY (group_name, then deleted-last, then user_name)
	// by accumulating in first-seen order.
	byID := map[uuid.UUID]*api.CatalogReportGroup{}
	order := []uuid.UUID{}
	for _, r := range rows {
		g, ok := byID[r.GroupID]
		if !ok {
			g = &api.CatalogReportGroup{
				ID:        r.GroupID.String(),
				Name:      r.GroupName,
				GroupType: r.GroupType,
				Members:   []api.CatalogReportGroupMember{},
			}
			byID[r.GroupID] = g
			order = append(order, r.GroupID)
		}

		member := api.CatalogReportGroupMember{
			UserID:            r.UserID.String(),
			Name:              r.UserName,
			Email:             r.Email,
			UserType:          r.UserType,
			MembershipAddedAt: r.MembershipAddedAt.Format(time.RFC3339),
		}
		if r.LastSignedInAt != nil {
			s := r.LastSignedInAt.Format(time.RFC3339)
			member.LastSignedInAt = &s
		}
		if r.MembershipDeletedAt != nil {
			s := r.MembershipDeletedAt.Format(time.RFC3339)
			member.MembershipDeletedAt = &s
		}
		g.Members = append(g.Members, member)
	}

	out := make([]api.CatalogReportGroup, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
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
		if out.Audit.Groups == nil {
			out.Audit.Groups = []api.CatalogReportGroup{}
		}
	}
	return out
}
