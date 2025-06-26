package v1

import (
	"github.com/flanksource/duty/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flanksource/incident-commander/api"
)

// ViewSummaryDef defines a summary calculation for the view
type ViewSummaryDef struct {
	Query types.AggregatedResourceSelector `json:"query" yaml:"query"`

	// Name of the summary
	Name string `json:"name" yaml:"name"`

	// Description of what this summary represents
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Table to query for the summary. (default: configs)
	// +kubebuilder:validation:Enum=configs;changes
	Table string `json:"table" yaml:"table"`

	// Type of summary visualization (piechart, text, gauge, number)
	// +kubebuilder:validation:Enum=piechart;text;gauge;number;breakdown
	Type api.ViewSummaryType `json:"type" yaml:"type"`
}

// ViewQuery defines a query configuration for populating the view
type ViewQuery struct {
	// Selector defines the resource selector for finding matching resources
	Selector types.ResourceSelector `json:"selector" yaml:"selector"`

	// Max number of results to return
	Max int `json:"max,omitempty" yaml:"max,omitempty"`

	// Mapping defines how to map query results to view columns
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	Mapping map[string]types.CelExpression `json:"mapping" yaml:"mapping" template:"true"`
}

// ViewQueriesSpec defines the structure for different types of queries
type ViewQueriesSpec struct {
	Configs []ViewQuery `json:"configs,omitempty" yaml:"configs,omitempty"`
	Changes []ViewQuery `json:"changes,omitempty" yaml:"changes,omitempty"`
}

// ViewSpec defines the desired state of View
type ViewSpec struct {
	// Summary calculations for the view
	Summary []ViewSummaryDef `json:"summary,omitempty" yaml:"summary,omitempty"`

	// Columns define the structure of the view
	Columns []api.ViewColumnDef `json:"columns" yaml:"columns"`

	// Queries define the queries and mappings to populate the view
	Queries ViewQueriesSpec `json:"queries" yaml:"queries"`
}

// ViewStatus defines the observed state of View
type ViewStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// View is the schema for the Views API
type View struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   ViewSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status ViewStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ViewList contains a list of View
type ViewList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []View `json:"items"`
}

func init() {
	SchemeBuilder.Register(&View{}, &ViewList{})
}
