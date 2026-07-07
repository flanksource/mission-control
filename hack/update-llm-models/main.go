package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/llm"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "update-llm-models: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var output string
	var source string
	flag.StringVar(&output, "output", "", "Path to write the generated pricing registry")
	flag.StringVar(&source, "source", llm.ModelsDevAPIURL, "models.dev API URL, local file path, or - for stdin")
	flag.Parse()

	if output == "" {
		return fmt.Errorf("--output is required")
	}

	data, err := readSource(source)
	if err != nil {
		return err
	}

	registry, err := llm.GeneratePricingRegistryFromModelsDev(data)
	if err != nil {
		return err
	}

	if dir := filepath.Dir(output); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	if err := os.WriteFile(output, registry, 0644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}

	return nil
}

func readSource(source string) ([]byte, error) {
	switch {
	case source == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		return readURL(source)
	default:
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", source, err)
		}
		return data, nil
	}
}

func readURL(source string) ([]byte, error) {
	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "mission-control/update-llm-models")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", source, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	return data, nil
}
