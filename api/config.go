package api

import (
	"github.com/flanksource/incident-commander/db/types"
	"github.com/google/uuid"
)

type ConfigItem struct {
	ID     uuid.UUID     `json:"id"`
	Config types.JSONMap `json:"config"`
}
