package api

import (
	"time"

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

	// List of all possible values for each column where filter is enabled.
	ColumnOptions map[string][]string `json:"columnOptions,omitempty"`
}
