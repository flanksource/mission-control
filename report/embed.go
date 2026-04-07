// Package report exposes the embedded TSX source files for the facet renderer.
package report

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed *.tsx *.ts  package.json tsconfig.json components
var FS embed.FS

// SourceDir overrides the embedded report files with a local directory or file.
// When set to a directory, facet renders use it directly instead of extracting
// embedded files. When set to a file, the file's directory is used and the
// filename overrides the entry file.
var SourceDir string

// ResolveSource returns the source directory and entry file override.
// If SourceDir points to a file, returns (dir, basename).
// If SourceDir points to a directory or is empty, returns (SourceDir, "").
func ResolveSource() (dir string, entryFile string) {
	if SourceDir == "" {
		return "", ""
	}
	info, err := os.Stat(SourceDir)
	if err != nil {
		return SourceDir, ""
	}
	if !info.IsDir() {
		return filepath.Dir(SourceDir), filepath.Base(SourceDir)
	}
	return SourceDir, ""
}
