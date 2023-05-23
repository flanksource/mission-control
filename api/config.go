package api

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

type ConfigItem struct {
	ID     uuid.UUID     `json:"id"`
	Config types.JSONMap `json:"config"`
}

type ConfigAnalysis struct {
	ID           uuid.UUID `json:"id"`
	Status       string    `json:"status"`
	LastObserved time.Time `json:"last_observed"`
}

func (t *ConfigAnalysis) TableName() string {
	return "config_analysis"
}

func (t ConfigAnalysis) AsMap() map[string]any {
	m := make(map[string]any)
	b, _ := json.Marshal(t)
	_ = json.Unmarshal(b, &m)

	m["age"] = time.Since(t.LastObserved)
	return m
}
