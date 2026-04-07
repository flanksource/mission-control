package catalog_report

import (
	"encoding/json"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/api"
)

func Export(ctx context.Context, configs []models.ConfigItem, opts Options, format string) ([]byte, error) {
	report, err := BuildReport(ctx, configs, opts)
	if err != nil {
		return nil, err
	}

	ctx.Logger.V(3).Infof("Report built: %d entries, %d changes, %d analyses",
		len(report.Entries), len(report.Changes), len(report.Analyses))

	switch format {
	case "html", "facet-html":
		return RenderFacetHTML(ctx, report)
	case "pdf", "facet-pdf":
		return RenderFacetPDF(ctx, report)
	default:
		return json.MarshalIndent(report, "", "  ")
	}
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
	return out
}
