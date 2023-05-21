package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Check struct {
	ID                 uuid.UUID `json:"id"`
	LastTransitionTime time.Time `json:"last_transition_time"`
	LastRuntime        time.Time `json:"last_runtime"`
	CreatedAt          time.Time `json:"created_at"`
	Status             string    `json:"status"`
}

// AsMap returns a map[string]any representation of the Check object
// with some additional fields.
func (t Check) AsMap() map[string]any {
	m := make(map[string]any)
	b, _ := json.Marshal(t)
	_ = json.Unmarshal(b, &m)

	m["age"] = time.Since(t.LastTransitionTime)
	return m
}
