package models

import (
	"time"

	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

type Team struct {
	ID        uuid.UUID `gorm:"default:generate_ulid();primaryKey"`
	Name      string    `gorm:"not null"`
	Icon      *string
	Spec      types.JSONMap
	Source    *string
	CreatedBy uuid.UUID  `gorm:"not null"`
	CreatedAt time.Time  `gorm:"type:timestamp with time zone;default:now();not null"`
	UpdatedAt time.Time  `gorm:"type:timestamp with time zone;default:now();not null"`
	DeletedAt *time.Time `gorm:"type:timestamp with time zone"`
}

type TeamMember struct {
	TeamID   uuid.UUID `gorm:"not null"`
	PersonID uuid.UUID `gorm:"not null"`
}
