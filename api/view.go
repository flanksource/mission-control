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
	Name        string               `json:"name" yaml:"name"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`
	Type        ViewSummaryType      `json:"type" yaml:"type"`
	Rows        []types.AggregateRow `json:"rows" yaml:"rows"`
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
