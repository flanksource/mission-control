package api

import (
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

// +kubebuilder:object:generate=true
type LLMContextRequest struct {
	// The config id to operate on.
	// If not provided, the playbook's config is used.
	Config string `json:"config,omitempty" yaml:"config,omitempty" template:"true"`

	// Select changes for the config to provide as an additional context to the AI model.
	Changes *TimeMetadata `json:"changes,omitempty" yaml:"changes,omitempty"`

	// Select analysis for the config to provide as an additional context to the AI model.
	Analysis *TimeMetadata `json:"analysis,omitempty" yaml:"analysis,omitempty"`

	// Select related configs to provide as an additional context to the AI model.
	Relationships []LLMContextRequestRelationship `json:"relationships,omitempty" yaml:"relationships,omitempty"`

	// List of playbooks that provide additional context to the LLM.
	Playbooks []LLMContextRequestPlaybook `json:"playbooks,omitempty" yaml:"playbooks,omitempty" template:"true"`
}

func (t LLMContextRequest) ShouldFetchConfigChanges() bool {
	// if changes are being fetched from relationships, we don't have to query
	// the changes for just the config alone.
	if t.Changes == nil {
		return false
	}

	if t.Changes.Since == "" {
		return false
	}

	for _, r := range t.Relationships {
		if r.Changes.Since != "" {
			return false
		}
	}

	return true
}

type TimeMetadata struct {
	// Since is a duration string.
	// Example: 4h, 30m
	Since string `json:"since" yaml:"since"`
}

// +kubebuilder:object:generate=true
type LLMContextRequestRelationship struct {
	// max depth to traverse the relationship. Defaults to 3
	Depth *int `json:"depth,omitempty"`

	// use incoming/outgoing/all relationships.
	Direction query.RelationDirection `json:"direction,omitempty"`

	Changes  TimeMetadata `json:"changes,omitempty"`
	Analysis TimeMetadata `json:"analysis,omitempty"`
}

func (t LLMContextRequestRelationship) ToRelationshipQuery(configID uuid.UUID) query.RelationQuery {
	q := query.RelationQuery{
		ID:       configID,
		MaxDepth: t.Depth,
		Relation: t.Direction,
	}

	if q.MaxDepth == nil {
		q.MaxDepth = lo.ToPtr(3)
	}

	if q.Relation == "" {
		q.Relation = query.All
	}

	return q
}

// LLMContextRequestPlaybook is a playbook that provides additional context to the LLM.
// This playbook is run before calling the LLM and it's output is added to the context.
// +kubebuilder:object:generate=true
type LLMContextRequestPlaybook struct {
	// Namespace of the playbook
	Namespace string `json:"namespace" yaml:"namespace"`

	// Name of the playbook
	Name string `json:"name" yaml:"name"`

	// If is a CEL expression that decides if this playbook should be included in the context
	If string `json:"if,omitempty" yaml:"if,omitempty"`

	// Parameters to pass to the playbook
	Params map[string]string `json:"params,omitempty" yaml:"params,omitempty" template:"true"`
}
