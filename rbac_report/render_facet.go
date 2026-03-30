package rbac_report

import (
	"fmt"

	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
)

func RenderFacetHTML(ctx context.Context, r *api.RBACReport, byUser bool) ([]byte, error) {
	return renderWithFacet(ctx, r, "html", byUser)
}

func RenderFacetPDF(ctx context.Context, r *api.RBACReport, byUser bool) ([]byte, error) {
	return renderWithFacet(ctx, r, "pdf", byUser)
}

func renderWithFacet(ctx context.Context, r *api.RBACReport, format string, byUser bool) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("RBAC report must not be nil")
	}

	entryFile := "RBACReport.tsx"
	if byUser {
		entryFile = "RBACByUserReport.tsx"
	}

	ctx.Logger.V(3).Infof("Rendering facet-%s", format)

	result, err := report.RenderCLI(initSlices(r), format, entryFile)
	if err != nil {
		return nil, err
	}

	ctx.Logger.V(3).Infof("Facet rendered %dKB of %s", len(result)/1024, format)
	return result, nil
}

func initSlices(r *api.RBACReport) api.RBACReport {
	out := *r
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
