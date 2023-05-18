package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Check struct {
	ID                 uuid.UUID `json:"id"`
	LastTransitionTime time.Time `json:"last_transition_time"`
	Status             string    `json:"status"`
}

// AsMap returns a map[string]any representation of the Check object
// with some additional fields.
func (t Check) AsMap() map[string]any {
	m := make(map[string]any)
	b, _ := json.Marshal(t)
	_ = json.Unmarshal(b, &m)

	transitionDuration := time.Since(t.LastTransitionTime)
	m["transition_duration_sec"] = int(transitionDuration.Seconds())
	m["transition_duration_min"] = int(transitionDuration.Minutes())
	return m
}
