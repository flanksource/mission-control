// Package report exposes the embedded TSX source files for the facet renderer.
package report

import "embed"

//go:embed *.tsx *.ts  package.json tsconfig.json components
var FS embed.FS
