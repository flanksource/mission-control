// Package report exposes the embedded TSX source files for the facet renderer.
package report

import "embed"

//go:embed Application.tsx types.ts mission-control.ts package.json tsconfig.json components
var FS embed.FS
