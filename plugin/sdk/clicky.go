package sdk

import (
	"encoding/json"
	"fmt"
)

// ClickyResultMimeType is the content-type plugins should use for operation
// results that contain clicky-renderable output.
const ClickyResultMimeType = "application/clicky+json"

// ClickyResult marshals an operation result for transport. Plugin handlers can
// return JSON-serializable values or json.RawMessage for already-encoded data.
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
