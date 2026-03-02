package application

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	icapi "github.com/flanksource/incident-commander/api"
)

func testApp() *icapi.Application {
	return &icapi.Application{
		ApplicationDetail: icapi.ApplicationDetail{
			ID:        "test-id",
			Name:      "test-app",
			Type:      "Platform",
			Namespace: "default",
			CreatedAt: time.Now(),
		},
		Incidents: []icapi.ApplicationIncident{},
		Backups:   []icapi.ApplicationBackup{},
		Restores:  []icapi.ApplicationBackupRestore{},
		Findings:  []icapi.ApplicationFinding{},
		Sections:  []icapi.ApplicationSection{},
		Locations: []icapi.ApplicationLocation{},
	}
}

func TestRenderFacetHTML(t *testing.T) {
	if _, err := exec.LookPath("facet"); err != nil {
		t.Skip("facet not on PATH; skipping facet-html integration test")
	}

	data, err := RenderFacetHTML(testApp())
	if err != nil {
		t.Fatalf("RenderFacetHTML: %v", err)
	}
	if !strings.Contains(string(data), "<html") {
		t.Errorf("expected HTML output, got: %.200s", string(data))
	}
}

func TestRenderFacetPDF(t *testing.T) {
	if _, err := exec.LookPath("facet"); err != nil {
		t.Skip("facet not on PATH; skipping facet-pdf integration test")
	}

	data, err := RenderFacetPDF(testApp())
	if err != nil {
		if strings.Contains(err.Error(), "Failed to launch the browser") {
			t.Skip("headless Chrome unavailable in this environment; skipping facet-pdf test")
		}
		t.Fatalf("RenderFacetPDF: %v", err)
	}
	// PDF files start with "%PDF"
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Errorf("expected PDF output, got first bytes: %.20s", string(data))
	}
}
