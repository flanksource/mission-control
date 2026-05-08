package application

import (
	"fmt"

	icapi "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
)

func RenderFacetHTML(app *icapi.Application) ([]byte, error) {
	if app == nil {
		return nil, fmt.Errorf("application must not be nil")
	}
	result, err := report.RenderCLI(initSlices(app), "html", "Application.tsx")
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func RenderFacetPDF(app *icapi.Application) ([]byte, error) {
	if app == nil {
		return nil, fmt.Errorf("application must not be nil")
	}
	result, err := report.RenderCLI(initSlices(app), "pdf", "Application.tsx")
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
