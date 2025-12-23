package api

import (
	"github.com/flanksource/duty/dataquery"
	pkgView "github.com/flanksource/duty/view"
)

// PanelType defines the type of panel visualization
type PanelType string

const (
	PanelTypePiechart   PanelType = "piechart"
	PanelTypeTable      PanelType = "table"
	PanelTypeText       PanelType = "text"
	PanelTypeNumber     PanelType = "number"
	PanelTypeDuration   PanelType = "duration"
	PanelTypeGauge      PanelType = "gauge"
	PanelTypeBargauge   PanelType = "bargauge"
	PanelTypeProperties PanelType = "properties"
	PanelTypeTimeseries PanelType = "timeseries"
)

// PanelDef defines a panel for the view
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.type!='gauge' ? !has(self.gauge) : true",message="gauge config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='piechart' ? !has(self.piechart) : true",message="piechart config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='number' ? !has(self.number) : true",message="number config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='table' ? !has(self.table) : true",message="table config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='bargauge' ? !has(self.bargauge) : true",message="bargauge config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='timeseries' ? !has(self.timeseries) : true",message="timeseries config not allowed for this type"
type PanelDef struct {
	PanelMeta `json:",inline" yaml:",inline"`

	// Query is a raw SQL query that has access to the queries as tables
	Query string `json:"query" yaml:"query"`
}

// +kubebuilder:object:generate=true
type PanelMeta struct {
	// Name of the panel
	Name string `json:"name" yaml:"name"`

	// Description of what this panel represents
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Type of panel visualization
	// +kubebuilder:validation:Enum=piechart;text;gauge;number;table;duration;bargauge;properties;timeseries
	Type PanelType `json:"type" yaml:"type"`

	// Configuration for gauge visualization
	Gauge *PanelGaugeConfig `json:"gauge,omitempty" yaml:"gauge,omitempty"`

	// Configuration for piechart visualization
	Piechart *PiechartConfig `json:"piechart,omitempty" yaml:"piechart,omitempty"`

	// Configuration for number visualization
	Number *PanelNumberConfig `json:"number,omitempty" yaml:"number,omitempty"`

	// Configuration for breakdown visualization
	Table *PanelTableConfig `json:"table,omitempty" yaml:"table,omitempty"`

	// Configuration for bargauge visualization
	Bargauge *PanelBargaugeConfig `json:"bargauge,omitempty" yaml:"bargauge,omitempty"`

	// Configuration for timeseries visualization
	Timeseries *PanelTimeseriesConfig `json:"timeseries,omitempty" yaml:"timeseries,omitempty"`
}

// +kubebuilder:object:generate=true
type PanelGaugeConfig struct {
	pkgView.GaugeConfig `json:",inline" yaml:",inline"`
	Unit                string `json:"unit,omitempty" yaml:"unit,omitempty"`
}

// PanelBargaugeConfig defines configuration for bargauge visualization
// +kubebuilder:object:generate=true
type PanelBargaugeConfig struct {
	pkgView.GaugeConfig `json:",inline" yaml:",inline"`
	Unit                string `json:"unit,omitempty" yaml:"unit,omitempty"`
	Group               string `json:"group,omitempty" yaml:"group,omitempty"`
	// Format defines how to display the value (percentage, multiplier)
	// +kubebuilder:validation:Enum=percentage;multiplier
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
}

// PiechartConfig defines configuration for piechart visualization
// +kubebuilder:object:generate=true
type PiechartConfig struct {
	ShowLabels bool              `json:"showLabels,omitempty" yaml:"showLabels,omitempty"`
	Colors     map[string]string `json:"colors,omitempty" yaml:"colors,omitempty"`
}

// PanelNumberConfig defines configuration for number visualization
// +kubebuilder:object:generate=true
type PanelNumberConfig struct {
	Unit      string `json:"unit,omitempty" yaml:"unit,omitempty"`
	Precision int    `json:"precision,omitempty" yaml:"precision,omitempty"`
}

// PanelTableConfig defines configuration for table visualization
// +kubebuilder:object:generate=true
type PanelTableConfig struct {
}

// PanelTimeseriesLegendConfig defines configuration for the timeseries legend
// +kubebuilder:object:generate=true
type PanelTimeseriesLegendConfig struct {
	// Whether to show the legend (default: true)
	Enable *bool `json:"enable,omitempty" yaml:"enable,omitempty"`
	// Orientation of the legend layout (default: horizontal)
	// +kubebuilder:validation:Enum=vertical;horizontal
	Layout string `json:"layout,omitempty" yaml:"layout,omitempty"`
}

// PanelTimeseriesConfig defines configuration for timeseries visualization
// +kubebuilder:object:generate=true
type PanelTimeseriesConfig struct {
	// Field name that contains the timestamp. If omitted, the panel will try to infer it.
	TimeKey string `json:"timeKey,omitempty" yaml:"timeKey,omitempty"`
	// Visualization style for the timeseries chart (default: lines)
	// +kubebuilder:validation:Enum=lines;area;points
	Style string `json:"style,omitempty" yaml:"style,omitempty"`
	// Convenience for single-series charts when series is not provided.
	ValueKey string `json:"valueKey,omitempty" yaml:"valueKey,omitempty"`
	// Legend configuration for the timeseries chart.
	Legend *PanelTimeseriesLegendConfig `json:"legend,omitempty" yaml:"legend,omitempty"`
}

type PanelResult struct {
	PanelMeta `json:",inline" yaml:",inline"`
	Rows      []dataquery.QueryResultRow `json:"rows" yaml:"rows"`
}
