package api

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

// PlaybookListItem is the response struct for listing playbooks
// for a filter/selector.
type PlaybookListItem struct {
	ID          uuid.UUID  `json:"id"`
	Namespace   string     `json:"namespace,omitempty"`
	Name        string     `json:"name"`
	Title       string     `json:"title,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	Description string     `json:"description,omitempty"`
	Source      string     `json:"source,omitempty"`
	Category    string     `json:"category,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	Parameters  types.JSON `json:"parameters,omitempty"`
	Spec        types.JSON `json:"spec,omitempty"`
}
