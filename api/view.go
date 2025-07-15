package api

import (
	"time"

	"github.com/flanksource/duty/view"
)

// ViewResult is the result of a view query
type ViewResult struct {
	LastRefreshedAt time.Time            `json:"lastRefreshedAt,omitempty"`
	Columns         []view.ViewColumnDef `json:"columns,omitempty"`
	Rows            []view.Row           `json:"rows,omitempty"`
	Panels          []PanelResult        `json:"panels,omitempty"`
}
