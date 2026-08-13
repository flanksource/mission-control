package clientcmd

import (
	"strconv"
	"strings"

	"github.com/flanksource/clicky/api"
)

// BoolFlag reads a boolean out of the raw flag map clicky hands to entity
// Get handlers, falling back to def when the flag is absent or unparseable.
func BoolFlag(flags map[string]string, key string, def bool) bool {
	raw, ok := flags[key]
	if !ok || raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

// EchoFilterLookup turns a comma-separated filter value back into the
// key/label map clicky.Filter.Lookup expects, for filters that are applied
// server-side and therefore have no local option catalog.
func EchoFilterLookup(value string) map[string]api.Textable {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make(map[string]api.Textable, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out[p] = api.Text{Content: p}
	}
	return out
}
