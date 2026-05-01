package ui

import _ "embed"

//go:generate go run ./internal/gen-checksum

//go:embed frontend/dist/ui.js
var bundleJS string

//go:embed frontend/dist/incident-commander-ui.css
var bundleCSS string

//go:embed assets/logo.svg
var logoSVG []byte

//go:embed assets/favicon.svg
var faviconSVG []byte
