package catalog_report

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/flanksource/clicky/api"
	"sigs.k8s.io/yaml"
)

const EmbeddedSettingsSource = "embedded defaults"

//go:embed default-settings.yaml
var defaultSettingsYAML []byte

type Settings struct {
	Filters          []string            `json:"filters,omitempty" yaml:"filters,omitempty"`
	Thresholds       SettingsThresholds  `json:"thresholds,omitempty" yaml:"thresholds,omitempty"`
	CategoryMappings map[string][]string `json:"categoryMappings,omitempty" yaml:"categoryMappings,omitempty"`
}

type SettingsThresholds struct {
	StaleDays         int `json:"staleDays,omitempty" yaml:"staleDays,omitempty"`
	ReviewOverdueDays int `json:"reviewOverdueDays,omitempty" yaml:"reviewOverdueDays,omitempty"`
}

func (s *Settings) Clone() *Settings {
	if s == nil {
		return &Settings{}
	}

	out := &Settings{
		Thresholds: s.Thresholds,
	}

	if len(s.Filters) > 0 {
		out.Filters = append([]string(nil), s.Filters...)
	}

	if len(s.CategoryMappings) > 0 {
		out.CategoryMappings = make(map[string][]string, len(s.CategoryMappings))
		for key, values := range s.CategoryMappings {
			out.CategoryMappings[key] = append([]string(nil), values...)
		}
	}

	return out
}

func parseSettings(data []byte, source string, base *Settings) (*Settings, error) {
	settings := base.Clone()
	if err := yaml.Unmarshal(data, settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings %s: %w", source, err)
	}
	return settings, nil
}

func LoadDefaultSettings() (*Settings, error) {
	return parseSettings(defaultSettingsYAML, EmbeddedSettingsSource, nil)
}

func LoadSettings(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings file %s: %w", path, err)
	}
	return parseSettings(data, path, nil)
}

func ResolveSettings(path string) (*Settings, string, error) {
	defaults, err := LoadDefaultSettings()
	if err != nil {
		return nil, "", err
	}

	if path == "" {
		return defaults, EmbeddedSettingsSource, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings file %s: %w", path, err)
	}

	settings, err := parseSettings(data, path, defaults)
	if err != nil {
		return nil, "", err
	}

	return settings, fmt.Sprintf("%s + %s", EmbeddedSettingsSource, path), nil
}

func (s *Settings) Pretty() api.Text {
	if s == nil {
		return api.Text{Content: "<none>", Style: "text-gray-500"}
	}
	items := []api.KeyValuePair{}
	if len(s.Filters) > 0 {
		items = append(items, api.KeyValue("Filters", strings.Join(s.Filters, ", ")))
	}
	if s.Thresholds.StaleDays > 0 || s.Thresholds.ReviewOverdueDays > 0 {
		items = append(items, api.KeyValue("Stale", fmt.Sprintf("%dd", s.Thresholds.StaleDays)))
		items = append(items, api.KeyValue("Review Overdue", fmt.Sprintf("%dd", s.Thresholds.ReviewOverdueDays)))
	}
	if len(s.CategoryMappings) > 0 {
		keys := make([]string, 0, len(s.CategoryMappings))
		for k := range s.CategoryMappings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var cats []string
		for _, k := range keys {
			cats = append(cats, fmt.Sprintf("%s: %s", k, strings.Join(s.CategoryMappings[k], ", ")))
		}
		items = append(items, api.KeyValue("Categories", strings.Join(cats, " | ")))
	}
	return api.Text{}.Add(api.DescriptionList{Items: items})
}

// FilterQuery returns the filters as a single search query string
// that can be appended to the ResourceSelector search.
func (s *Settings) FilterQuery() string {
	if s == nil || len(s.Filters) == 0 {
		return ""
	}
	return strings.Join(s.Filters, " ")
}
