package api

import (
	"strings"
	"time"
)

// ConfigChanges consists of data about changes to
// components, configs, analysis.
type ConfigChanges struct {
	PreviousCheck time.Time `json:"previous_check"`
	CheckedAt     time.Time `json:"checked_at"`
	Labels        map[string]string
	Components    []Component
	ConfigItems   []ConfigItem
}

type UpstreamConfig struct {
	URL      string
	Username string
	Password string
	Labels   []string
}

func (t *UpstreamConfig) IsFilled() bool {
	return t.URL != "" && t.Username != "" && t.Password != ""
}

func (t *UpstreamConfig) LabelsMap() map[string]string {
	return sanitizeStringSliceVar(t.Labels)
}

func sanitizeStringSliceVar(in []string) map[string]string {
	sanitized := make(map[string]string, len(in))
	for _, item := range in {
		splits := strings.SplitN(item, "=", 2)
		if len(splits) == 1 {
			continue // ignore this item. not in a=b format
		}

		sanitized[strings.TrimSpace(splits[0])] = strings.TrimSpace(splits[1])
	}

	return sanitized
}
