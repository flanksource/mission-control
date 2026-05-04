package ui

import (
	"embed"
	"io/fs"
)

//go:generate go run ./internal/gen-checksum

// frontendDist holds the entire vite build output (index.html + hashed
// assets/* + source maps). Vite emits multiple files now that we no longer
// bundle into a single IIFE; go:embed handles the whole tree natively.
//
//go:embed all:frontend/dist
var frontendDist embed.FS

//go:embed assets/logo.svg
var logoSVG []byte

//go:embed assets/favicon.svg
var faviconSVG []byte

// distFS returns the dist/ subtree rooted so paths like "index.html" and
// "assets/foo-abc123.js" resolve directly.
func distFS() fs.FS {
	sub, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		panic(err)
	}
	return sub
}
