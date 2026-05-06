package decompile

import (
	"encoding/json"
	"strings"
)

// extractFirstString pulls the first usable string payload out of an arthas
// `Results []any` slice. Arthas's HTTP response shape varies across versions:
// `jad` may emit a single string element, a map with a `body`/`text`/`message`
// key, or a slice of strings. Anything else falls back to a JSON-marshalled
// representation.
//
// Returns ("", false) when the slice is empty or yields no non-empty value.
func extractFirstString(results []any) (string, bool) {
	for _, item := range results {
		if s, ok := stringFrom(item); ok && strings.TrimSpace(s) != "" {
			return s, true
		}
	}
	return "", false
}

func stringFrom(item any) (string, bool) {
	switch v := item.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case []any:
		var sb strings.Builder
		for _, e := range v {
			s, ok := stringFrom(e)
			if !ok {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(s)
		}
		if sb.Len() == 0 {
			return "", false
		}
		return sb.String(), true
	case map[string]any:
		for _, key := range []string{"source", "body", "text", "message", "value"} {
			if raw, ok := v[key]; ok {
				if s, ok := stringFrom(raw); ok {
					return s, true
				}
			}
		}
		// Last resort: JSON-encode the map.
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(data), true
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}
