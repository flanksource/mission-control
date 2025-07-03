package api

import "time"

type ViewColumnType string

const (
	ViewColumnTypeString   ViewColumnType = "string"
	ViewColumnTypeNumber   ViewColumnType = "number"
	ViewColumnTypeBoolean  ViewColumnType = "boolean"
	ViewColumnTypeDateTime ViewColumnType = "datetime"
	ViewColumnTypeDuration ViewColumnType = "duration"
	ViewColumnTypeHealth   ViewColumnType = "health"
	ViewColumnTypeStatus   ViewColumnType = "status"
	ViewColumnTypeGauge    ViewColumnType = "gauge"
)

// ViewRow represents a single row of data mapped to view columns
type ViewRow []any

// ViewColumnDef defines a column in the view
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.type=='gauge' ? has(self.gauge) : !has(self.gauge)",message="gauge config required when type is gauge, not allowed for other types"
type ViewColumnDef struct {
	// Name of the column
	Name string `json:"name" yaml:"name"`

	// +kubebuilder:validation:Enum=string;number;boolean;datetime;duration;health;status;gauge
	Type ViewColumnType `json:"type" yaml:"type"`

	// Description of the column
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Configuration for gauge visualization
	Gauge *GaugeConfig `json:"gauge,omitempty" yaml:"gauge,omitempty"`
}

// ViewResult is the result of a view query
type ViewResult struct {
	LastRefreshedAt time.Time       `json:"lastRefreshedAt"`
	Columns         []ViewColumnDef `json:"columns,omitempty"`
	Rows            []ViewRow       `json:"rows,omitempty"`
	Panels          []PanelResult   `json:"panels,omitempty"`
}
