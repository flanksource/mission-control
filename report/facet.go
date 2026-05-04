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
	"strings"
	"sync"

	commonshttp "github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
)

// RenderResult contains the rendered output and metadata about the render.
type RenderResult struct {
	Data     []byte
	SrcDir   string
	Entry    string
	DataFile string
}

// RenderCLI renders data to the given format using the local facet CLI binary.
// With -v (log level 1): prints the facet command and tees stdout/stderr.
// With -vv (log level 2): also keeps the data file and report dir for re-rendering.
func RenderCLI(data any, format, entryFile string) (*RenderResult, error) {
	verbose := logger.IsLevelEnabled(1)
	keepFiles := logger.IsLevelEnabled(2)

	facetBin, err := exec.LookPath("facet")
	if err != nil {
		return nil, fmt.Errorf("facet not found on PATH: install with 'npm install -g @flanksource/facet'")
	}

	srcDir, err := SrcDir()
	if err != nil {
		return nil, fmt.Errorf("prepare facet src dir: %w", err)
	}
	if _, override := ResolveSource(); override != "" {
		entryFile = override
	}

	dataJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}

	dataFile, err := os.CreateTemp("", "facet-data-*.json")
	if err != nil {
		return nil, fmt.Errorf("create data temp file: %w", err)
	}
	if !keepFiles {
		defer os.Remove(dataFile.Name())
	}

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

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(facetBin, format, entryFile, "-d", dataFile.Name(), "-o", outFile.Name())
	cmd.Dir = srcDir

	fmt.Fprintf(os.Stderr, "$ cd %s\n", srcDir)
	fmt.Fprintf(os.Stderr, "$ %s\n", strings.Join(cmd.Args, " "))
	cmd.Stdout = io.MultiWriter(&stdout, os.Stderr)
	cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)

	if verbose {
		fmt.Fprintf(os.Stderr, "facet report source=%s entry=%s data=%s output=%s\n", srcDir, entryFile, dataFile.Name(), outFile.Name())
	}

	if err = cmd.Run(); err != nil {
		return nil, facetCommandError(format, err, stdout.String(), stderr.String())
	}

	result, err := os.ReadFile(outFile.Name())
	if err != nil {
		return nil, err
	}

	return &RenderResult{
		Data:     result,
		SrcDir:   srcDir,
		Entry:    entryFile,
		DataFile: dataFile.Name(),
	}, nil
}

func facetCommandError(format string, err error, stdout string, stderr string) error {
	var sections []string
	if strings.TrimSpace(stdout) != "" {
		sections = append(sections, "stdout:\n"+strings.TrimRight(stdout, "\n"))
	}
	if strings.TrimSpace(stderr) != "" {
		sections = append(sections, "stderr:\n"+strings.TrimRight(stderr, "\n"))
	}
	if len(sections) == 0 {
		return fmt.Errorf("facet %s failed: %w", format, err)
	}
	return fmt.Errorf("facet %s failed: %w\n%s", format, err, strings.Join(sections, "\n\n"))
}

type RenderHTTPOptions struct {
	TimestampURL string
}

// RenderHTTP renders data via a remote facet rendering service.
func RenderHTTP(ctx context.Context, baseURL, token string, data any, format, entryFile string, opts ...RenderHTTPOptions) ([]byte, error) {
	archive, err := BuildArchive()
	if err != nil {
		return nil, fmt.Errorf("build report archive: %w", err)
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}

	renderOpts := map[string]any{
		"format":    format,
		"entryFile": entryFile,
	}
	if len(opts) > 0 && opts[0].TimestampURL != "" {
		renderOpts["timestampUrl"] = opts[0].TimestampURL
	}
	optionsJSON, err := json.Marshal(renderOpts)
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
	if dir, _ := ResolveSource(); dir != "" {
		return dir, nil
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
	if err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
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
		if path == "package.json" {
			data, err = sanitizeReportPackageJSON(data, true)
			if err != nil {
				return err
			}
		}
		return os.WriteFile(dest, data, 0600)
	}); err != nil {
		return err
	}

	return cleanupFacetInstallState(destDir)
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
		if path == "package.json" {
			data, err = sanitizeReportPackageJSON(data, false)
			if err != nil {
				return err
			}
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

func sanitizeReportPackageJSON(data []byte, allowLocalLinks bool) ([]byte, error) {
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse report package.json: %w", err)
	}

	pnpm, ok := manifest["pnpm"].(map[string]any)
	if !ok {
		return data, nil
	}

	overrides, ok := pnpm["overrides"].(map[string]any)
	if ok {
		for name, raw := range overrides {
			value, ok := raw.(string)
			if !ok {
				continue
			}
			if isLocalPackageRef(value) {
				if !allowLocalLinks {
					delete(overrides, name)
					continue
				}
				rewritten, ok := rewriteLocalPackageRef(value)
				if !ok {
					delete(overrides, name)
					continue
				}
				overrides[name] = rewritten
			}
		}
		if len(overrides) == 0 {
			delete(pnpm, "overrides")
		}
	}

	if len(pnpm) == 0 {
		delete(manifest, "pnpm")
	}

	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("write report package.json: %w", err)
	}
	return append(out, '\n'), nil
}

func isLocalPackageRef(value string) bool {
	if strings.HasPrefix(value, "link:") || strings.HasPrefix(value, "file:") || filepath.IsAbs(value) {
		return true
	}
	return false
}

func rewriteLocalPackageRef(value string) (string, bool) {
	prefix := ""
	path := value
	switch {
	case strings.HasPrefix(value, "link:"):
		prefix = "link:"
		path = strings.TrimPrefix(value, "link:")
	case strings.HasPrefix(value, "file:"):
		prefix = "file:"
		path = strings.TrimPrefix(value, "file:")
	case filepath.IsAbs(value):
		prefix = "link:"
	default:
		return value, true
	}

	if filepath.IsAbs(path) {
		if pathExists(path) {
			return prefix + path, true
		}
		return "", false
	}

	for _, base := range reportSourcePathCandidates() {
		candidate := filepath.Clean(filepath.Join(base, path))
		if pathExists(candidate) {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return "", false
			}
			return prefix + abs, true
		}
	}

	return "", false
}

func reportSourcePathCandidates() []string {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "report"),
			cwd,
		)
	}
	return candidates
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cleanupFacetInstallState(destDir string) error {
	_ = os.Remove(filepath.Join(destDir, "pnpm-lock.yaml"))

	facetDir := filepath.Join(destDir, ".facet")
	_ = os.Remove(filepath.Join(facetDir, "package.json"))
	_ = os.Remove(filepath.Join(facetDir, "pnpm-lock.yaml"))

	nodeModules := filepath.Join(facetDir, "node_modules")
	for _, path := range []string{
		filepath.Join(nodeModules, "@flanksource", "facet"),
		filepath.Join(nodeModules, "@flanksource", "clicky-ui"),
	} {
		if hasBrokenSymlink(path) {
			return os.RemoveAll(nodeModules)
		}
	}
	return nil
}

func hasBrokenSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	_, err = filepath.EvalSymlinks(path)
	return err != nil
}
