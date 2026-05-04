package ui

import _ "embed"

//go:embed openapi.json
var openapiJSON []byte

// OpenAPIJSON returns the stub OpenAPI document describing the RPC endpoints
// the embedded UI calls. Served at /schemas/openapi.json so clicky-ui's
// EntityExplorerApp can list and execute operations without a separate
// spec-generation pipeline.
//
// Expand this file as new operations are introduced; it's a hand-maintained
// stub, not a full description of mission-control's API.
func OpenAPIJSON() []byte {
	return openapiJSON
}
