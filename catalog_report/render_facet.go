package catalog_report

import (
	"fmt"

	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
)

func RenderFacetHTML(ctx context.Context, r *api.CatalogReport) ([]byte, error) {
	return renderWithFacet(ctx, r, "html")
}

func RenderFacetPDF(ctx context.Context, r *api.CatalogReport) ([]byte, error) {
	return renderWithFacet(ctx, r, "pdf")
}

func renderWithFacet(ctx context.Context, r *api.CatalogReport, format string) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("catalog report must not be nil")
	}

	ctx.Logger.V(3).Infof("Rendering catalog facet-%s", format)

	data := initSlices(r)
	result, err := report.RenderCLI(data, format, "CatalogReport.tsx")
	if err != nil {
		return nil, err
	}

	ctx.Logger.V(3).Infof("Facet rendered %dKB of %s", len(result)/1024, format)
	return result, nil
}
