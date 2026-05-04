package sdk

import (
	"encoding/json"
	"fmt"
)

// ClickyResultMimeType is the content-type plugins should use for any
// operation result that contains clicky-renderable output. The host
// proxies the bytes verbatim and the CLI/UI know how to render it.
const ClickyResultMimeType = "application/clicky+json"

// ClickyResult marshals an operation result for transport. Plugin handlers
// typically return a domain struct that implements Pretty() (see the clicky
// package); the SDK wraps it in a ClickyResultMimeType payload. Callers can
// also pass json.RawMessage for already-encoded payloads.
//
// The marshaling intentionally goes through encoding/json (not the clicky
// renderer) — rendering happens on the receiving side, where it has access
// to the user's terminal capabilities.
func ClickyResult(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}

	if raw, ok := v.(json.RawMessage); ok {
		return []byte(raw), nil
	}

	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("clicky result marshal: %w", err)
	}
	return b, nil
}
