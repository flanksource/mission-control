package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	icapi "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/report"
)

func RenderFacetHTML(app *icapi.Application) ([]byte, error) {
	return renderWithFacet(app, "html")
}

func RenderFacetPDF(app *icapi.Application) ([]byte, error) {
	return renderWithFacet(app, "pdf")
}

func renderWithFacet(app *icapi.Application, format string) ([]byte, error) {
	facetBin, err := exec.LookPath("facet")
	if err != nil {
		return nil, fmt.Errorf("facet not found on PATH: install with 'npm install -g @flanksource/facet'")
	}

	srcDir, err := facetSrcDir()
	if err != nil {
		return nil, fmt.Errorf("prepare facet src dir: %w", err)
	}

	dataJSON, err := json.MarshalIndent(initSlices(app), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal application: %w", err)
	}

	dataFile, err := os.CreateTemp("", "facet-data-*.json")
	if err != nil {
		return nil, fmt.Errorf("create data temp file: %w", err)
	}
	defer os.Remove(dataFile.Name())

	if _, err := dataFile.Write(dataJSON); err != nil {
		return nil, fmt.Errorf("write data file: %w", err)
	}
	dataFile.Close()

	ext := format
	if format == "html" {
		ext = "html"
	}
	outFile := filepath.Join(srcDir, "output."+ext)

	var stderr bytes.Buffer
	cmd := exec.Command(facetBin, format, "Application.tsx", "-d", dataFile.Name(), "-o", "output."+ext)
	cmd.Dir = srcDir
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("facet %s failed: %w\n%s", format, err, stderr.String())
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		entries, _ := os.ReadDir(srcDir)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return nil, fmt.Errorf("read facet output %s: %w (srcDir contains: %s)", outFile, err, strings.Join(names, ", "))
	}

	return data, nil
}

// facetSrcDir returns a stable directory containing the embedded report TSX files.
// On first call it extracts the files; subsequent calls reuse the directory so that
// facet can cache its .facet/node_modules there across invocations.
func facetSrcDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "incident-commander", "facet-report")

	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	// Always (re)extract so embedded changes are picked up on binary upgrade.
	if err := extractReportFiles(dir); err != nil {
		return "", err
	}

	return dir, nil
}

// initSlices returns a shallow copy of app with nil slices replaced by empty
// slices so the TSX renderer receives [] instead of null.
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

func extractReportFiles(destDir string) error {
	return fs.WalkDir(report.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		dest := filepath.Join(destDir, path)
		if d.IsDir() {
			return os.MkdirAll(dest, 0750)
		}
		data, err := report.FS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	})
}
