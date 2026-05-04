package sdk

import (
	"encoding/json"
	"net/http"
)

// BuildInfo describes a plugin's build metadata. Plugin main packages
// declare these as ldflag-injected vars and pass them through Manifest().
//
//	var (
//	    Version   = "dev"
//	    BuildDate = "unknown"
//	)
//
// Then, in Manifest():
//
//	Version: sdk.FormatVersion(Version, BuildDate, uiChecksum),
type BuildInfo struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	BuildDate  string `json:"buildDate"`
	UIChecksum string `json:"uiChecksum,omitempty"`
}

// FormatVersion produces the canonical version string embedded in the
// gRPC manifest: "<version>+<uiChecksum[:8]> built <buildDate>". The
// supervisor's startup log and the host's plugin registry rely on this
// shape — keep the order stable.
func FormatVersion(version, buildDate, uiChecksum string) string {
	out := version
	if uiChecksum != "" && len(uiChecksum) >= 8 {
		out += "+" + uiChecksum[:8]
	}
	if buildDate != "" {
		out += " built " + buildDate
	}
	return out
}

// VersionHandler returns an HTTP handler that serves a plugin's BuildInfo
// as JSON at any path the plugin mounts it on (typically "/version").
// The returned handler is safe to mount at root since it ignores the
// request URL.
func VersionHandler(info BuildInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	})
}
