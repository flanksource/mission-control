package api

import (
	"github.com/google/uuid"
)

// PlaybookListItem is the response struct for listing playbooks
// for a filter/selector.
type PlaybookListItem struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
