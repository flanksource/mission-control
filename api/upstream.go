package api

import (
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

var TablesToReconcile = []string{
	"components",
	"config_scrapers",
	"config_items",
	"canaries",
	"checks",
	"topologies",
}

type PaginateRequest struct {
	Table string    `query:"table"`
	From  uuid.UUID `query:"from"`
	Size  int       `query:"size"`
}

type PaginateResponse struct {
	Hash  string    `gorm:"column:sha256sum"`
	Next  uuid.UUID `gorm:"column:last_id"`
	Total int       `gorm:"column:total"`
}

var UpstreamConf UpstreamConfig

// PushData consists of data about changes to
// components, configs, analysis.
type PushData struct {
	AgentName                    string                               `json:"cluster_name,omitempty"`
	Canaries                     []models.Canary                      `json:"canaries,omitempty"`
	Checks                       []models.Check                       `json:"checks,omitempty"`
	Components                   []models.Component                   `json:"components,omitempty"`
	ConfigScrapers               []models.ConfigScraper               `json:"config_scrapers,omitempty"`
	ConfigAnalysis               []models.ConfigAnalysis              `json:"config_analysis,omitempty"`
	ConfigChanges                []models.ConfigChange                `json:"config_changes,omitempty"`
	ConfigItems                  []models.ConfigItem                  `json:"config_items,omitempty"`
	CheckStatuses                []models.CheckStatus                 `json:"check_statuses,omitempty"`
	ConfigRelationships          []models.ConfigRelationship          `json:"config_relationships,omitempty"`
	ComponentRelationships       []models.ComponentRelationship       `json:"component_relationships,omitempty"`
	ConfigComponentRelationships []models.ConfigComponentRelationship `json:"config_component_relationships,omitempty"`
	Topologies                   []models.Topology                    `json:"topologies,omitempty"`
}

func (p *PushData) String() string {
	result := ""
	result += fmt.Sprintf("AgentName: %s\n", p.AgentName)
	result += fmt.Sprintf("Topologies: %d\n", len(p.Topologies))
	result += fmt.Sprintf("Canaries: %d\n", len(p.Canaries))
	result += fmt.Sprintf("Checks: %d\n", len(p.Checks))
	result += fmt.Sprintf("Components: %d\n", len(p.Components))
	result += fmt.Sprintf("ConfigAnalysis: %d\n", len(p.ConfigAnalysis))
	result += fmt.Sprintf("ConfigScrapers: %d\n", len(p.ConfigScrapers))
	result += fmt.Sprintf("ConfigChanges: %d\n", len(p.ConfigChanges))
	result += fmt.Sprintf("ConfigItems: %d\n", len(p.ConfigItems))
	result += fmt.Sprintf("CheckStatuses: %d\n", len(p.CheckStatuses))
	result += fmt.Sprintf("ConfigRelationships: %d\n", len(p.ConfigRelationships))
	result += fmt.Sprintf("ComponentRelationships: %d\n", len(p.ComponentRelationships))
	result += fmt.Sprintf("ConfigComponentRelationships: %d\n", len(p.ConfigComponentRelationships))
	return result
}

// ReplaceTopologyID replaces the topology_id for all the components
// with the provided id.
func (t *PushData) ReplaceTopologyID(id *uuid.UUID) {
	for i := range t.Components {
		t.Components[i].TopologyID = id
	}
}

// PopulateAgentID sets agent_id on all the data
func (t *PushData) PopulateAgentID(id uuid.UUID) {
	for i := range t.Canaries {
		t.Canaries[i].AgentID = id
	}
	for i := range t.Checks {
		t.Checks[i].AgentID = id
	}
	for i := range t.Components {
		t.Components[i].AgentID = id
	}
	for i := range t.ConfigItems {
		t.ConfigItems[i].AgentID = id
	}
	for i := range t.ConfigScrapers {
		t.ConfigScrapers[i].AgentID = id
	}
	for i := range t.Topologies {
		t.Topologies[i].AgentID = id
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

	for i := range t.Topologies {
		t.Topologies[i].Labels = collections.MergeMap(t.Topologies[i].Labels, labels)
	}
}

type UpstreamConfig struct {
	AgentName string
	Host      string
	Username  string
	Password  string
	Labels    []string
}

func (t *UpstreamConfig) Valid() bool {
	return t.Host != "" && t.Username != "" && t.Password != "" && t.AgentName != ""
}

func (t *UpstreamConfig) IsPartiallyFilled() bool {
	return !t.Valid() && (t.Host != "" || t.Username != "" || t.Password != "" || t.AgentName != "")
}

func (t *UpstreamConfig) LabelsMap() map[string]string {
	return collections.KeyValueSliceToMap(t.Labels)
}
