package rbac_report

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
)

func RenderFacetHTML(ctx context.Context, r *api.RBACReport, view string) ([]byte, error) {
	return renderWithFacet(ctx, r, "html", view)
}

func RenderFacetPDF(ctx context.Context, r *api.RBACReport, view string) ([]byte, error) {
	return renderWithFacet(ctx, r, "pdf", view)
}

func renderWithFacet(ctx context.Context, r *api.RBACReport, format string, view string) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("RBAC report must not be nil")
	}

	entryFile := "RBACMatrixReport.tsx"
	switch view {
	case "user":
		entryFile = "RBACByUserReport.tsx"
	}

	ctx.Logger.V(3).Infof("Rendering facet-%s", format)

	result, err := report.RenderCLI(initSlices(r), format, entryFile)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to render RBAC %s report", format)
	}

	ctx.Logger.V(3).Infof("Facet rendered %dKB of %s", len(result.Data)/1024, format)
	return result.Data, nil
}

func initSlices(r *api.RBACReport) api.RBACReport {
	out := *r
	if out.Parents == nil {
		out.Parents = []models.ConfigItem{}
	}
	if out.Resources == nil {
		out.Resources = []api.RBACResource{}
	}
	if out.Changelog == nil {
		out.Changelog = []api.RBACChangeEntry{}
	}
	for i := range out.Resources {
		if out.Resources[i].Users == nil {
			out.Resources[i].Users = []api.RBACUserRole{}
		}
	}
	if out.Users == nil {
		out.Users = []api.RBACUserReport{}
	}
	for i := range out.Users {
		if out.Users[i].Resources == nil {
			out.Users[i].Resources = []api.RBACUserResource{}
		}
	}
	return out
}
