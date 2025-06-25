package api

import "github.com/flanksource/duty/types"

type ViewColumnType string

const (
	ViewColumnTypeString   ViewColumnType = "string"
	ViewColumnTypeNumber   ViewColumnType = "number"
	ViewColumnTypeBoolean  ViewColumnType = "boolean"
	ViewColumnTypeDateTime ViewColumnType = "datetime"
	ViewColumnTypeDuration ViewColumnType = "duration"
)

// ViewRow represents a single row of data mapped to view columns
type ViewRow []any

// ViewSummaryType defines the type of summary visualization
type ViewSummaryType string

const (
	ViewSummaryTypePiechart  ViewSummaryType = "piechart"
	ViewSummaryTypeBreakdown ViewSummaryType = "breakdown"
	ViewSummaryTypeText      ViewSummaryType = "text"
	ViewSummaryTypeNumber    ViewSummaryType = "number"
	ViewSummaryTypeGauge     ViewSummaryType = "gauge"
)

type ViewResult struct {
	Columns   []ViewColumnDef `json:"columns,omitempty"`
	Rows      []ViewRow       `json:"rows,omitempty"`
	Summaries []SummaryResult `json:"summaries,omitempty"`
}

type SummaryResult struct {
	ViewSummaryMeta `json:",inline" yaml:",inline"`
	Rows            []types.AggregateRow `json:"rows" yaml:"rows"`
}

// ViewColumnDef defines a column in the view
type ViewColumnDef struct {
	// Name of the column
	Name string `json:"name" yaml:"name"`

	// +kubebuilder:validation:Enum=string;number;boolean;datetime;duration
	Type ViewColumnType `json:"type" yaml:"type"`

	// Description of the column
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// GaugeThreshold defines a threshold configuration for gauge charts
// +kubebuilder:object:generate=true
type GaugeThreshold struct {
	Value int    `json:"value" yaml:"value"`
	Color string `json:"color" yaml:"color"`
}

// GaugeConfig defines configuration for gauge visualization
// +kubebuilder:object:generate=true
type GaugeConfig struct {
	Min        int              `json:"min" yaml:"min"`
	Max        int              `json:"max" yaml:"max"`
	Thresholds []GaugeThreshold `json:"thresholds,omitempty" yaml:"thresholds,omitempty"`
}

// PiechartConfig defines configuration for piechart visualization
// +kubebuilder:object:generate=true
type PiechartConfig struct {
	ShowLabels bool     `json:"showLabels,omitempty" yaml:"showLabels,omitempty"`
	Colors     []string `json:"colors,omitempty" yaml:"colors,omitempty"`
}

// NumberConfig defines configuration for number visualization
// +kubebuilder:object:generate=true
type NumberConfig struct {
	Unit      string `json:"unit,omitempty" yaml:"unit,omitempty"`
	Precision int    `json:"precision,omitempty" yaml:"precision,omitempty"`
}

// BreakdownConfig defines configuration for breakdown visualization
// +kubebuilder:object:generate=true
type BreakdownConfig struct {
}

// ViewSummaryDef defines a summary calculation for the view
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.type=='gauge' ? has(self.gauge) : !has(self.gauge)",message="gauge config required when type is gauge, not allowed for other types"
// +kubebuilder:validation:XValidation:rule="self.type!='piechart' ? !has(self.piechart) : true",message="piechart config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='number' ? !has(self.number) : true",message="number config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='breakdown' ? !has(self.breakdown) : true",message="breakdown config not allowed for this type"
type ViewSummaryDef struct {
	ViewSummaryMeta `json:",inline" yaml:",inline"`

	// +kubebuilder:validation:Enum=configs;changes
	Source string `json:"source" yaml:"source"`

	Query types.AggregatedResourceSelector `json:"query" yaml:"query"`
}

// +kubebuilder:object:generate=true
type ViewSummaryMeta struct {
	// Name of the summary
	Name string `json:"name" yaml:"name"`

	// Description of what this summary represents
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Type of summary visualization (piechart, text, gauge, number)
	// +kubebuilder:validation:Enum=piechart;text;gauge;number;breakdown
	Type ViewSummaryType `json:"type" yaml:"type"`

	// Configuration for gauge visualization
	Gauge *GaugeConfig `json:"gauge,omitempty" yaml:"gauge,omitempty"`

	// Configuration for piechart visualization
	Piechart *PiechartConfig `json:"piechart,omitempty" yaml:"piechart,omitempty"`

	// Configuration for number visualization
	Number *NumberConfig `json:"number,omitempty" yaml:"number,omitempty"`

	// Configuration for breakdown visualization
	Breakdown *BreakdownConfig `json:"breakdown,omitempty" yaml:"breakdown,omitempty"`
}
