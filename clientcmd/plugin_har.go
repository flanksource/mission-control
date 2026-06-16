package clientcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/flanksource/commons/har"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/pkg/httpobservability"
)

// pluginHARPath is the value of the global --har flag. When non-empty,
// every plugin command captures outbound HTTP traffic into a HAR file at
// this path.
var pluginHARPath string

// harActive guards against nested HAR activation: once a capture is running
// (e.g. faro's global StartHAR), inner activations (the plugin dispatch path)
// become no-ops so the active collector captures their traffic too.
var harActive bool

// StartHAR activates HAR capture for the configured --har path and returns a
// flush that restores the previous collector and writes the file. It is
// idempotent: when --har is unset or a capture is already active it returns a
// no-op. faro wraps command execution with this so every command's HTTP is
// captured; the flush should run unconditionally (including on error).
func StartHAR() func() error {
	collector, flush := startHAR()
	_ = collector
	return flush
}

// startHAR returns a fresh HAR collector if --har was passed (and no capture is
// already active) plus a writer closure that persists it once the command
// finishes; otherwise returns (nil, no-op).
func startHAR() (*har.Collector, func() error) {
	if pluginHARPath == "" || harActive {
		return nil, func() error { return nil }
	}
	harActive = true
	collector := har.NewCollector(har.HARConfig{
		MaxBodySize:         64 * 1024,
		CaptureContentTypes: []string{"application/json", "application/clicky+json", "application/x-www-form-urlencoded"},
	})
	restore := httpobservability.SetHARCollector(collector)
	return collector, func() error {
		harActive = false
		restore()
		return writeHAR(pluginHARPath, collector)
	}
}

func writeHAR(path string, collector *har.Collector) error {
	if collector == nil {
		return nil
	}
	file := har.File{
		Log: har.Log{
			Version: "1.2",
			Creator: har.Creator{Name: "mission-control-cli", Version: time.Now().UTC().Format(time.RFC3339)},
			Entries: collector.Entries(),
		},
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode HAR: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write HAR %s: %w", path, err)
	}
	return nil
}

// registerPluginHARFlag attaches --har to the given root command. Called
// once at init() time.
func registerPluginHARFlag(root *cobra.Command) {
	root.PersistentFlags().StringVar(&pluginHARPath, "har", "",
		"Write outbound HTTP traffic as a HAR file at this path")
}
