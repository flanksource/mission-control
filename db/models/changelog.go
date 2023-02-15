package models

import (
	"time"

	"github.com/google/uuid"
)

type Changelog struct {
	ID        uuid.UUID
	ItemID    uuid.UUID `gorm:"column:item_id"`
	Timestamp time.Time `gorm:"column:tstamp"`
	TableName string    `gorm:"column:tablename"`
}
