package catalog_report

import (
	"fmt"

	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
)

func renderFacetResult(ctx context.Context, r *api.CatalogReport, format string) (data []byte, srcDir, entry, dataFile string, err error) {
	if r == nil {
		return nil, "", "", "", fmt.Errorf("catalog report must not be nil")
	}

	ctx.Logger.V(3).Infof("Rendering catalog facet-%s", format)

	result, err := report.RenderCLI(initSlices(r), format, "CatalogReport.tsx")
	if err != nil {
		return nil, "", "", "", err
	}

	ctx.Logger.V(3).Infof("Facet rendered %dKB of %s", len(result.Data)/1024, format)
	return result.Data, result.SrcDir, result.Entry, result.DataFile, nil
}

func RenderFacetHTML(ctx context.Context, r *api.CatalogReport) ([]byte, error) {
	data, _, _, _, err := renderFacetResult(ctx, r, "html")
	return data, err
}

func RenderFacetPDF(ctx context.Context, r *api.CatalogReport) ([]byte, error) {
	data, _, _, _, err := renderFacetResult(ctx, r, "pdf")
	return data, err
}
