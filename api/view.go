package api

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/view"
	"github.com/google/uuid"
)

// DisplayTable defines table specific display options
// +kubebuilder:object:generate=true
type DisplayTable struct {
	// Sort defines the default sort column (single field). Prefix with "-" for descending.
	// +kubebuilder:validation:Pattern=`^[+-]?[A-Za-z0-9_.]+$`
	Sort string `json:"sort,omitempty" yaml:"sort,omitempty"`

	// Size defines the default page size for the table
	Size int `json:"size,omitempty" yaml:"size,omitempty"`
}

// DisplayCard defines card layout configuration at the view level
type DisplayCard struct {
	// Columns defines the number of columns for the card body layout
	Columns int `json:"columns,omitempty" yaml:"columns,omitempty"`

	// Default indicates if this is the default card layout
	Default bool `json:"default,omitempty" yaml:"default,omitempty"`
}

// ViewListItem is the response to listing views for a config selector.
type ViewListItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Title     string    `json:"title,omitempty"`
	Icon      string    `json:"icon,omitempty"`
}

// ColumnFilterOptions holds filter options for a column.
// For regular columns, List contains distinct values.
// For labels columns, Labels contains keys mapped to their possible values.
type ColumnFilterOptions struct {
	List   []string            `json:"list,omitempty"`
	Labels map[string][]string `json:"labels,omitempty"`
}

type ViewRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ViewSection is a section rendered in the application view
type ViewSection struct {
	Title   string  `json:"title"`
	Icon    string  `json:"icon,omitempty"`
	ViewRef ViewRef `json:"viewRef"`
}

// ViewResult is the result of a view query
type ViewResult struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`

	LastRefreshedAt    time.Time        `json:"lastRefreshedAt"`
	RequestFingerprint string           `json:"requestFingerprint,omitempty"`
	Columns            []view.ColumnDef `json:"columns,omitempty"`
	Rows               []view.Row       `json:"rows,omitempty"`
	Panels             []PanelResult    `json:"panels,omitempty"`

	Variables []ViewVariableWithOptions `json:"variables,omitempty"`

	// List of all possible values for each column where filter is enabled.
	ColumnOptions map[string]ColumnFilterOptions `json:"columnOptions,omitempty"`

	// Card defines the default card layout configuration
	Card *DisplayCard `json:"card,omitempty"`

	// Table defines the default table display configuration
	Table *DisplayTable `json:"table,omitempty"`

	Sections []ViewSection `json:"sections,omitempty"`
}

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(has(self.values) && !has(self.valueFrom)) || (!has(self.values) && has(self.valueFrom))",message="exactly one of values or valueFrom is required"
//
// A variable represents a dynamic parameter that can be substituted into data queries.
// Modifying the variable's value will update all elements that reference it.
// Variables appear as interactive controls (dropdown menus or text inputs) at the top of the view interface.
type ViewVariable struct {
	// Key is the unique identifier of the variable.
	// The variable's value is accessible in the dataquery templates as $(var.<key>)
	Key string `json:"key" yaml:"key"`

	// Label is the human-readable name of the variable.
	Label string `json:"label" yaml:"label"`

	// Values is the list of values that the variable can take.
	Values []string `json:"values,omitempty" yaml:"values,omitempty"`

	// ValueFrom is the source of the variable's values.
	ValueFrom *ViewVariableValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`

	// Default is the default value of the variable.
	Default string `json:"default,omitempty" yaml:"default,omitempty"`

	// Variables this variable depends on - must be resolved before this variable can be populated
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
}

// +kubebuilder:object:generate=true
type ViewVariableValueFrom struct {
	Config types.ResourceSelector `json:"config" yaml:"config"`
}

type ViewVariableWithOptions struct {
	ViewVariable `json:",inline" yaml:",inline"`
	Options      []string `json:"options" yaml:"options"`
}
