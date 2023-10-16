package api

import (
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

// PlaybookListItem is the response struct for listing playbooks
// for a filter/selector.
type PlaybookListItem struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Parameters types.JSON `json:"parameters,omitempty"`
}
