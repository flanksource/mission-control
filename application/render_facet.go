package application

import (
	"fmt"

	"github.com/flanksource/duty/context"

	icapi "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
)

func RenderFacetHTML(ctx context.Context, app *icapi.Application) ([]byte, error) {
	return renderWithFacet(ctx, app, "html")
}

func RenderFacetPDF(ctx context.Context, app *icapi.Application) ([]byte, error) {
	return renderWithFacet(ctx, app, "pdf")
}

func renderWithFacet(ctx context.Context, app *icapi.Application, format string) ([]byte, error) {
	if app == nil {
		return nil, fmt.Errorf("application must not be nil")
	}

	result, err := report.Render(ctx, initSlices(app), format, "Application.tsx", "", nil)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func initSlices(app *icapi.Application) icapi.Application {
	out := *app
	if out.Incidents == nil {
		out.Incidents = []icapi.ApplicationIncident{}
	}
	if out.Backups == nil {
		out.Backups = []icapi.ApplicationBackup{}
	}
	if out.Restores == nil {
		out.Restores = []icapi.ApplicationBackupRestore{}
	}
	if out.Findings == nil {
		out.Findings = []icapi.ApplicationFinding{}
	}
	if out.Sections == nil {
		out.Sections = []icapi.ApplicationSection{}
	}
	if out.Locations == nil {
		out.Locations = []icapi.ApplicationLocation{}
	}
	if out.AccessControl.Users == nil {
		out.AccessControl.Users = []icapi.UserAndRole{}
	}
	if out.AccessControl.Authentication == nil {
		out.AccessControl.Authentication = []icapi.AuthMethod{}
	}
	return out
}
