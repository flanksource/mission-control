package api

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/view"
	"github.com/google/uuid"
)

// ViewListItem is the response to listing views for a config selector.
type ViewListItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Title     string    `json:"title,omitempty"`
	Icon      string    `json:"icon,omitempty"`
}

// ViewResult is the result of a view query
type ViewResult struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`

	LastRefreshedAt time.Time        `json:"lastRefreshedAt"`
	Columns         []view.ColumnDef `json:"columns,omitempty"`
	Rows            []view.Row       `json:"rows,omitempty"`
	Panels          []PanelResult    `json:"panels,omitempty"`

	Filters []ViewFilterParameterWithOptions `json:"filters,omitempty"`

	// List of all possible values for each column where filter is enabled.
	ColumnOptions map[string][]string `json:"columnOptions,omitempty"`
}

type ViewFilterType string

const (
	ViewFilterTypeDropdown ViewFilterType = "dropdown"
	ViewFilterTypeToggle   ViewFilterType = "toggle"
)

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(has(self.values) && !has(self.valueFrom)) || (!has(self.values) && has(self.valueFrom))",message="exactly one of values or valueFrom is required"
type ViewFilterParameter struct {
	// Key is the unique identifier of the filter.
	// The filter's value is accessible in the dataquery templates as {{ .filter.key }}.
	Key string `json:"key" yaml:"key"`

	// Label is the human-readable name of the filter.
	Label string `json:"label" yaml:"label"`

	// Values is the list of values that the filter can take.
	Values []string `json:"values,omitempty" yaml:"values,omitempty"`

	// ValueFrom is the source of the filter's values.
	ValueFrom *ViewFilterValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`

	// Default is the default value of the filter.
	Default string `json:"default" yaml:"default"`

	// Type is the type of the filter (default: dropdown).
	// +kubebuilder:validation:Enum=dropdown;toggle
	Type ViewFilterType `json:"type,omitempty" yaml:"type,omitempty"`
}

// +kubebuilder:object:generate=true
type ViewFilterValueFrom struct {
	Config types.ResourceSelector `json:"config" yaml:"config"`
}

type ViewFilterParameterWithOptions struct {
	ViewFilterParameter
	Options []string `json:"options" yaml:"options"`
}
