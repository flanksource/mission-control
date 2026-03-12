package views

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

func RenderFacetHTML(ctx context.Context, result *api.ViewResult) ([]byte, error) {
	return renderViewWithFacet(ctx, result, "html")
}

func RenderFacetPDF(ctx context.Context, result *api.ViewResult) ([]byte, error) {
	return renderViewWithFacet(ctx, result, "pdf")
}

func RenderMultiFacetHTML(ctx context.Context, multi *api.MultiViewResult) ([]byte, error) {
	return renderFacetWithData(ctx, multi, "html")
}

func RenderMultiFacetPDF(ctx context.Context, multi *api.MultiViewResult) ([]byte, error) {
	return renderFacetWithData(ctx, multi, "pdf")
}

func renderFacetWithData(ctx context.Context, data any, format string) ([]byte, error) {
	if data == nil {
		return nil, fmt.Errorf("data must not be nil")
	}

	facetBin, err := exec.LookPath("facet")
	if err != nil {
		return nil, fmt.Errorf("facet not found on PATH: install with 'npm install -g @flanksource/facet'")
	}

	srcDir, err := viewFacetSrcDir()
	if err != nil {
		return nil, fmt.Errorf("prepare facet src dir: %w", err)
	}

	dataJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}

	dataFile, err := os.CreateTemp("", "facet-view-data-*.json")
	if err != nil {
		return nil, fmt.Errorf("create data temp file: %w", err)
	}
	defer os.Remove(dataFile.Name())

	if _, err := dataFile.Write(dataJSON); err != nil {
		return nil, fmt.Errorf("write data file: %w", err)
	}
	dataFile.Close()

	outFile, err := os.CreateTemp("", "facet-view-output-*."+format)
	if err != nil {
		return nil, fmt.Errorf("create output temp file: %w", err)
	}
	outFile.Close()
	defer os.Remove(outFile.Name())

	ctx.Logger.V(3).Infof("facet binary: %s, data size: %dKB", facetBin, len(dataJSON)/1024)

	var stderr bytes.Buffer
	cmd := exec.Command(facetBin, format, "ViewReport.tsx", "-d", dataFile.Name(), "-o", outFile.Name())
	cmd.Dir = srcDir
	cmd.Stderr = &stderr

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

func renderViewWithFacet(ctx context.Context, result *api.ViewResult, format string) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("view result must not be nil")
	}

	facetBin, err := exec.LookPath("facet")
	if err != nil {
		return nil, fmt.Errorf("facet not found on PATH: install with 'npm install -g @flanksource/facet'")
	}

	srcDir, err := viewFacetSrcDir()
	if err != nil {
		return nil, fmt.Errorf("prepare facet src dir: %w", err)
	}

	dataJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal view result: %w", err)
	}

	ctx.Logger.V(3).Infof("facet binary: %s, data size: %dKB", facetBin, len(dataJSON)/1024)

	dataFile, err := os.CreateTemp("", "facet-view-data-*.json")
	if err != nil {
		return nil, fmt.Errorf("create data temp file: %w", err)
	}
	defer os.Remove(dataFile.Name())

	if _, err := dataFile.Write(dataJSON); err != nil {
		return nil, fmt.Errorf("write data file: %w", err)
	}
	dataFile.Close()

	outFile, err := os.CreateTemp("", "facet-view-output-*."+format)
	if err != nil {
		return nil, fmt.Errorf("create output temp file: %w", err)
	}
	outFile.Close()
	defer os.Remove(outFile.Name())

	var stderr bytes.Buffer
	cmd := exec.Command(facetBin, format, "ViewReport.tsx", "-d", dataFile.Name(), "-o", outFile.Name())
	cmd.Dir = srcDir
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("facet %s failed: %w\n%s", format, err, stderr.String())
	}

	output, err := os.ReadFile(outFile.Name())
	if err != nil {
		return nil, fmt.Errorf("read facet output: %w", err)
	}

	ctx.Logger.V(3).Infof("Facet rendered %dKB of %s", len(output)/1024, format)
	return output, nil
}

func viewFacetSrcDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "incident-commander", "facet-report")

	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	if err := viewExtractReportFiles(dir); err != nil {
		return "", err
	}

	return dir, nil
}

func viewExtractReportFiles(destDir string) error {
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
