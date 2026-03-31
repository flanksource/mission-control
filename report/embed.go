// Package report exposes the embedded TSX source files for the facet renderer.
package report

import "embed"

//go:embed *.tsx *.ts  package.json tsconfig.json components
var FS embed.FS

// SourceDir overrides the embedded report files with a local directory.
// When set, facet renders use this directory directly instead of extracting
// embedded files to a cache directory.
var SourceDir string
