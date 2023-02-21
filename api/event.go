package api

import (
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

type Event struct {
	ID         uuid.UUID
	Name       string
	Properties types.JSONStringMap `json:"properties" gorm:"type:jsonstringmap;<-:false"`
	Error      string
}

// We are using the term `Event` as it represents an event in the
// event_queue table, but the table is named event_queue
// to signify it's usage as a queue
func (Event) TableName() string {
	return "event_queue"
}
