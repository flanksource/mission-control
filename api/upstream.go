package api

import (
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

// PushData consists of data about changes to
// components, configs, analysis.
type PushData struct {
	Labels                       map[string]string                    `json:"labels"`
	ClusterName                  string                               `json:"cluster_name"`
	Canaries                     []models.Canary                      `json:"canaries"`
	Checks                       []models.Check                       `json:"checks"`
	Components                   []models.Component                   `json:"components"`
	ConfigAnalysis               []models.ConfigAnalysis              `json:"config_analysis"`
	ConfigChanges                []models.ConfigChange                `json:"config_changes"`
	ConfigItems                  []models.ConfigItem                  `json:"config_items"`
	CheckStatuses                []models.CheckStatus                 `json:"check_statuses"`
	ConfigRelationships          []models.ConfigRelationship          `json:"config_relationships"`
	ComponentRelationships       []models.ComponentRelationship       `json:"component_relationships"`
	ConfigComponentRelationships []models.ConfigComponentRelationship `json:"config_component_relationships"`
}

// ReplaceTemplateID replaces the template id for all the components
// with the provided id.
func (t *PushData) ReplaceTemplateID(id *uuid.UUID) {
	for i := range t.Components {
		t.Components[i].SystemTemplateID = id
	}
}

type UpstreamConfig struct {
	ClusterName string
	URL         string
	Username    string
	Password    string
	Labels      []string
}

func (t *UpstreamConfig) Valid() bool {
	return t.URL != "" && t.Username != "" && t.Password != "" && t.ClusterName != ""
}

func (t *UpstreamConfig) IsPartiallyFilled() bool {
	return !t.Valid() && (t.URL != "" || t.Username != "" || t.Password != "" || t.ClusterName != "")
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
