package rbac_report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

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

	facetBin, err := exec.LookPath("facet")
	if err != nil {
		return nil, fmt.Errorf("facet not found on PATH: install with 'npm install -g @flanksource/facet'")
	}

	srcDir, err := facetSrcDir()
	if err != nil {
		return nil, fmt.Errorf("prepare facet src dir: %w", err)
	}

	dataJSON, err := json.MarshalIndent(initSlices(r), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal RBAC report: %w", err)
	}

	ctx.Logger.V(3).Infof("facet binary: %s, data size: %dKB", facetBin, len(dataJSON)/1024)

	dataFile, err := os.CreateTemp("", "facet-rbac-data-*.json")
	if err != nil {
		return nil, fmt.Errorf("create data temp file: %w", err)
	}
	defer os.Remove(dataFile.Name())

	if _, err := dataFile.Write(dataJSON); err != nil {
		return nil, fmt.Errorf("write data file: %w", err)
	}
	dataFile.Close()

	outFile, err := os.CreateTemp("", "facet-rbac-output-*."+format)
	if err != nil {
		return nil, fmt.Errorf("create output temp file: %w", err)
	}
	outFile.Close()
	defer os.Remove(outFile.Name())

	entryFile := "RBACReport.tsx"
	if byUser {
		entryFile = "RBACByUserReport.tsx"
	}

	var stderr, stdout bytes.Buffer
	cmd := exec.Command(facetBin, format, entryFile, "-d", dataFile.Name(), "-o", outFile.Name())
	cmd.Dir = srcDir
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout

	ctx.Logger.V(3).Infof("Rendering facet-%s (%dKB data)", format, len(dataJSON)/1024)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("facet %s failed: %w\n%s", format, err, stderr.String())
	}

	result, err := os.ReadFile(outFile.Name())
	if err != nil {
		return nil, fmt.Errorf("read facet output: %w", err)
	}

	ctx.Logger.V(3).Infof("Facet rendered %dKB of %s", len(result)/1024, format)
	return result, nil
}

func facetSrcDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "incident-commander", "facet-report")

	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	if err := extractReportFiles(dir); err != nil {
		return "", err
	}

	return dir, nil
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
