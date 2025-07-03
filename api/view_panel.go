package api

import "github.com/flanksource/duty/types"

// PanelType defines the type of panel visualization
type PanelType string

const (
	PanelTypePiechart PanelType = "piechart"
	PanelTypeTable    PanelType = "table"
	PanelTypeText     PanelType = "text"
	PanelTypeNumber   PanelType = "number"
	PanelTypeGauge    PanelType = "gauge"
)

// PanelDef defines a panel for the view
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.type=='gauge' ? has(self.gauge) : !has(self.gauge)",message="gauge config required when type is gauge, not allowed for other types"
// +kubebuilder:validation:XValidation:rule="self.type!='piechart' ? !has(self.piechart) : true",message="piechart config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='number' ? !has(self.number) : true",message="number config not allowed for this type"
// +kubebuilder:validation:XValidation:rule="self.type!='table' ? !has(self.table) : true",message="table config not allowed for this type"
type PanelDef struct {
	PanelMeta `json:",inline" yaml:",inline"`

	// +kubebuilder:validation:Enum=configs;changes
	// Source specifies the data source of the panel
	Source string `json:"source" yaml:"source"`

	Query types.AggregatedResourceSelector `json:"query" yaml:"query"`
}

// +kubebuilder:object:generate=true
type PanelMeta struct {
	// Name of the panel
	Name string `json:"name" yaml:"name"`

	// Description of what this panel represents
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Type of panel visualization (piechart, text, gauge, number)
	// +kubebuilder:validation:Enum=piechart;text;gauge;number;table
	Type PanelType `json:"type" yaml:"type"`

	// Configuration for gauge visualization
	Gauge *GaugeConfig `json:"gauge,omitempty" yaml:"gauge,omitempty"`

	// Configuration for piechart visualization
	Piechart *PiechartConfig `json:"piechart,omitempty" yaml:"piechart,omitempty"`

	// Configuration for number visualization
	Number *PanelNumberConfig `json:"number,omitempty" yaml:"number,omitempty"`

	// Configuration for breakdown visualization
	Table *PanelTableConfig `json:"table,omitempty" yaml:"table,omitempty"`
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
	Min        int              `json:"min,omitempty" yaml:"min,omitempty"`
	Max        int              `json:"max,omitempty" yaml:"max,omitempty"`
	Thresholds []GaugeThreshold `json:"thresholds,omitempty" yaml:"thresholds,omitempty"`
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

type PanelResult struct {
	PanelMeta `json:",inline" yaml:",inline"`
	Rows      []types.AggregateRow `json:"rows" yaml:"rows"`
}
