package api

import (
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

// PushData consists of data about changes to
// components, configs, analysis.
type PushData struct {
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
		t.Components[i].TopologyID = id
	}
}

// ApplyLabels injects additional labels to the suitable fields
func (t *PushData) ApplyLabels(labels map[string]string) {
	for i := range t.Components {
		t.Components[i].Labels = collections.MergeMap(t.Components[i].Labels, labels)
	}

	for i := range t.Checks {
		t.Checks[i].Labels = collections.MergeMap(t.Checks[i].Labels, labels)
	}

	for i := range t.Canaries {
		t.Canaries[i].Labels = collections.MergeMap(t.Canaries[i].Labels, labels)
	}
}

type UpstreamConfig struct {
	ClusterName string
	Host        string
	Username    string
	Password    string
	Labels      []string
}

func (t *UpstreamConfig) Valid() bool {
	return t.Host != "" && t.Username != "" && t.Password != "" && t.ClusterName != ""
}

func (t *UpstreamConfig) IsPartiallyFilled() bool {
	return !t.Valid() && (t.Host != "" || t.Username != "" || t.Password != "" || t.ClusterName != "")
}

func (t *UpstreamConfig) LabelsMap() map[string]string {
	return collections.KeyValueSliceToMap(t.Labels)
}
