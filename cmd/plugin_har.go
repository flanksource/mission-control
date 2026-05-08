package cmd

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

// startHAR returns a fresh HAR collector if --har was passed and a writer
// closure that persists it once the command finishes; otherwise returns
// (nil, no-op).
func startHAR() (*har.Collector, func() error) {
	if pluginHARPath == "" {
		return nil, func() error { return nil }
	}
	collector := har.NewCollector(har.HARConfig{
		MaxBodySize:         64 * 1024,
		CaptureContentTypes: []string{"application/json", "application/clicky+json", "application/x-www-form-urlencoded"},
	})
	restore := httpobservability.SetHARCollector(collector)
	return collector, func() error {
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
		"Write outbound HTTP traffic as a HAR file (path) for plugin commands")
}
