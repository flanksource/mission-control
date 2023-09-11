package api

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

type Event struct {
	ID          uuid.UUID           `gorm:"default:generate_ulid()"`
	Name        string              `json:"name"`
	CreatedAt   time.Time           `json:"created_at"`
	Properties  types.JSONStringMap `json:"properties"`
	Error       string              `json:"error"`
	Attempts    int                 `json:"attempts"`
	LastAttempt *time.Time          `json:"last_attempt"`
	Priority    int                 `json:"priority"`
}

// We are using the term `Event` as it represents an event in the
// event_queue table, but the table is named event_queue
// to signify it's usage as a queue
func (Event) TableName() string {
	return "event_queue"
}
