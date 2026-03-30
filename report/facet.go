package report

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	commonshttp "github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"
)

// RenderCLI renders data to the given format using the local facet CLI binary.
func RenderCLI(data any, format, entryFile string) ([]byte, error) {
	facetBin, err := exec.LookPath("facet")
	if err != nil {
		return nil, fmt.Errorf("facet not found on PATH: install with 'npm install -g @flanksource/facet'")
	}

	srcDir, err := SrcDir()
	if err != nil {
		return nil, fmt.Errorf("prepare facet src dir: %w", err)
	}

	dataJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
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

	outFile, err := os.CreateTemp("", "facet-output-*."+format)
	if err != nil {
		return nil, fmt.Errorf("create output temp file: %w", err)
	}
	outFile.Close()
	defer os.Remove(outFile.Name())

	var stderr bytes.Buffer
	cmd := exec.Command(facetBin, format, entryFile, "-d", dataFile.Name(), "-o", outFile.Name())
	cmd.Dir = srcDir
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("facet %s failed: %w\n%s", format, err, stderr.String())
	}

	return os.ReadFile(outFile.Name())
}

// RenderHTTP renders data via a remote facet rendering service.
func RenderHTTP(ctx context.Context, baseURL, token string, data any, format, entryFile string) ([]byte, error) {
	archive, err := BuildArchive()
	if err != nil {
		return nil, fmt.Errorf("build report archive: %w", err)
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}

	optionsJSON, err := json.Marshal(map[string]any{
		"format":    format,
		"entryFile": entryFile,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal options: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	fw, err := mw.CreateFormFile("archive", "report.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("create archive form field: %w", err)
	}
	if _, err := fw.Write(archive); err != nil {
		return nil, fmt.Errorf("write archive field: %w", err)
	}

	if err := mw.WriteField("data", string(dataJSON)); err != nil {
		return nil, fmt.Errorf("write data field: %w", err)
	}

	if err := mw.WriteField("options", string(optionsJSON)); err != nil {
		return nil, fmt.Errorf("write options field: %w", err)
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	client := commonshttp.NewClient().BaseURL(baseURL)
	if token != "" {
		client = client.Header("X-API-Key", token)
	}

	response, err := client.R(ctx).
		Header("Content-Type", mw.FormDataContentType()).
		Post("/render", &body)
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

// SrcDir returns a stable directory containing the embedded report TSX files.
// On first call it extracts the files; subsequent calls reuse the directory.
var SrcDir = sync.OnceValues(func() (string, error) {
	if SourceDir != "" {
		return SourceDir, nil
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "incident-commander", "facet-report")

	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	if err := ExtractFiles(dir); err != nil {
		return "", err
	}

	return dir, nil
})

// ExtractFiles writes all embedded report files to destDir.
func ExtractFiles(destDir string) error {
	return fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
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
		data, err := FS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	})
}

// BuildArchive creates a tar.gz archive of all embedded report files.
func BuildArchive() ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := FS.ReadFile(path)
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
