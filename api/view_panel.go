package api

import (
	"github.com/flanksource/duty/dataquery"
	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
)

// PanelType defines the type of panel visualization
type PanelType string

const (
	PanelTypeBargauge   PanelType = "bargauge"
	PanelTypeDuration   PanelType = "duration"
	PanelTypeGauge      PanelType = "gauge"
	PanelTypeNumber     PanelType = "number"
	PanelTypePiechart   PanelType = "piechart"
	PanelTypePlaybooks  PanelType = "playbooks"
	PanelTypeProperties PanelType = "properties"
	PanelTypeTimeseries PanelType = "timeseries"
	PanelTypeTable      PanelType = "table"
	PanelTypeText       PanelType = "text"
)

// PanelDef defines a panel for the view
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.type!='gauge' ? !has(self.gauge) : true",message="gauge config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='piechart' ? !has(self.piechart) : true",message="piechart config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='number' ? !has(self.number) : true",message="number config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='table' ? !has(self.table) : true",message="table config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='bargauge' ? !has(self.bargauge) : true",message="bargauge config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='timeseries' ? !has(self.timeseries) : true",message="timeseries config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='playbooks' ? !has(self.playbooks) : true",message="playbooks config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='playbooks' ? has(self.query) : true",message="query is required for this panel type"
type PanelDef struct {
	PanelMeta `json:",inline" yaml:",inline"`

	// Query is a raw SQL query that has access to the queries as tables
	Query string `json:"query,omitempty" yaml:"query,omitempty"`
}

// +kubebuilder:object:generate=true
type PlaybooksPanelConfig struct {
	// Playbooks matching this selector will be displayed in the panel
	Selector types.ResourceSelector `json:"selector" yaml:"selector"`
}

// +kubebuilder:object:generate=true
type PanelMeta struct {
	// Name of the panel
	Name string `json:"name" yaml:"name"`

	// Description of what this panel represents
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Type of panel visualization
	// +kubebuilder:validation:Enum=piechart;text;gauge;number;table;duration;bargauge;properties;timeseries;playbooks
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
	// Configuration for playbooks panel
	Playbooks *PlaybooksPanelConfig `json:"playbooks,omitempty" yaml:"playbooks,omitempty"`
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
