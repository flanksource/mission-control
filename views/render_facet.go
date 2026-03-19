package views

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/view"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/report"
)

type facetViewSectionResult struct {
	Title string            `json:"title"`
	Icon  string            `json:"icon,omitempty"`
	Error string            `json:"error,omitempty"`
	View  *facetViewPayload `json:"view,omitempty"`
}

// facetViewPayload is used only for facet-html/facet-pdf rendering.
// api.ViewResult keeps Rows as json:"-" for regular API responses, but the facet
// report TSX reads table data from `rows`, so we inject Rows here without changing
// the public API shape. SectionResults is also wrapped so nested viewRef tables keep rows.
type facetViewPayload struct {
	*api.ViewResult
	Rows           []view.Row               `json:"rows,omitempty"`
	SectionResults []facetViewSectionResult `json:"sectionResults,omitempty"`
}

type facetMultiViewPayload struct {
	Views []facetViewPayload `json:"views"`
}

func newFacetViewPayload(result *api.ViewResult) *facetViewPayload {
	if result == nil {
		return nil
	}

	payload := &facetViewPayload{
		ViewResult: result,
		Rows:       result.Rows,
	}

	if len(result.SectionResults) > 0 {
		payload.SectionResults = make([]facetViewSectionResult, 0, len(result.SectionResults))
		for _, sr := range result.SectionResults {
			payload.SectionResults = append(payload.SectionResults, facetViewSectionResult{
				Title: sr.Title,
				Icon:  sr.Icon,
				Error: sr.Error,
				View:  newFacetViewPayload(sr.View),
			})
		}
	}

	return payload
}

func newFacetMultiViewPayload(result *api.MultiViewResult) *facetMultiViewPayload {
	if result == nil {
		return nil
	}

	payload := &facetMultiViewPayload{
		Views: make([]facetViewPayload, 0, len(result.Views)),
	}

	for i := range result.Views {
		viewPayload := newFacetViewPayload(&result.Views[i])
		if viewPayload != nil {
			payload.Views = append(payload.Views, *viewPayload)
		}
	}

	return payload
}

func RenderFacetHTML(ctx context.Context, result *api.ViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderViewWithFacet(ctx, result, "html", opts)
}

func RenderFacetPDF(ctx context.Context, result *api.ViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderViewWithFacet(ctx, result, "pdf", opts)
}

func RenderMultiFacetHTML(ctx context.Context, multi *api.MultiViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderFacetWithData(ctx, newFacetMultiViewPayload(multi), "html", opts)
}

func RenderMultiFacetPDF(ctx context.Context, multi *api.MultiViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderFacetWithData(ctx, newFacetMultiViewPayload(multi), "pdf", opts)
}

func resolveFacetConnection(ctx context.Context, opts *v1.FacetOptions) (baseURL, token, timestampURL string, err error) {
	if opts == nil {
		return "", "", "", nil
	}

	timestampURL = opts.TimestampURL

	if opts.Connection != "" {
		conn, err := connection.Get(ctx, opts.Connection)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to get facet connection: %w", err)
		}
		if conn == nil {
			return "", "", "", fmt.Errorf("facet connection %q not found", opts.Connection)
		}
		if conn.Type != models.ConnectionTypeFacet {
			return "", "", "", fmt.Errorf("connection %q is type %q, expected %q", opts.Connection, conn.Type, models.ConnectionTypeFacet)
		}
		baseURL = conn.URL
		token = conn.Password
		if timestampURL == "" {
			timestampURL = conn.Properties["timestampUrl"]
		}
	}

	if opts.URL != "" {
		baseURL = opts.URL
	}

	return baseURL, token, timestampURL, nil
}

func buildReportArchive() ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := fs.WalkDir(report.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := report.FS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: path,
			Size: int64(len(data)),
			Mode: 0600,
		}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderFacetHTTP(ctx context.Context, baseURL, token string, data any, format string, opts *v1.FacetOptions) ([]byte, error) {
	archive, err := buildReportArchive()
	if err != nil {
		return nil, fmt.Errorf("build report archive: %w", err)
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}

	params := url.Values{
		"format":    {format},
		"entryFile": {"ViewReport.tsx"},
	}

	client := http.NewClient().BaseURL(baseURL)
	if token != "" {
		client = client.Header("X-API-Key", token)
	}

	response, err := client.R(ctx).
		Header("Content-Type", "application/gzip").
		Header("X-Facet-Data", base64.StdEncoding.EncodeToString(dataJSON)).
		Post("/render?"+params.Encode(), archive)
	if err != nil {
		return nil, fmt.Errorf("facet render request failed: %w", err)
	}
	if !response.IsOK() {
		errBody, _ := response.AsString()
		return nil, fmt.Errorf("facet render failed (status %d): %s", response.StatusCode, errBody)
	}

	if format == "html" {
		return io.ReadAll(response.Body)
	}

	renderResult, err := response.AsJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to parse render response: %w", err)
	}
	resultURL, _ := renderResult["url"].(string)
	if resultURL == "" {
		return nil, fmt.Errorf("render response missing 'url' field")
	}

	pdfResponse, err := client.R(ctx).Get(resultURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rendered result: %w", err)
	}
	if !pdfResponse.IsOK() {
		errBody, _ := pdfResponse.AsString()
		return nil, fmt.Errorf("result fetch failed (status %d): %s", pdfResponse.StatusCode, errBody)
	}

	return io.ReadAll(pdfResponse.Body)
}

func renderFacetWithData(ctx context.Context, data any, format string, opts *v1.FacetOptions) ([]byte, error) {
	if data == nil {
		return nil, fmt.Errorf("data must not be nil")
	}

	baseURL, token, _, err := resolveFacetConnection(ctx, opts)
	if err != nil {
		return nil, err
	}
	if baseURL != "" {
		return renderFacetHTTP(ctx, baseURL, token, data, format, opts)
	}

	return renderFacetCLI(ctx, data, format)
}

func renderViewWithFacet(ctx context.Context, result *api.ViewResult, format string, opts *v1.FacetOptions) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("view result must not be nil")
	}

	payload := newFacetViewPayload(result)

	baseURL, token, _, err := resolveFacetConnection(ctx, opts)
	if err != nil {
		return nil, err
	}
	if baseURL != "" {
		return renderFacetHTTP(ctx, baseURL, token, payload, format, opts)
	}

	return renderFacetCLI(ctx, payload, format)
}

func renderFacetCLI(ctx context.Context, data any, format string) ([]byte, error) {
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
