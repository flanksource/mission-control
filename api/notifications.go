package api

import (
	"context"
	"database/sql/driver"

	"github.com/flanksource/duty/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type NotificationConfig struct {
	Filter     string            `json:"filter,omitempty"`                     // Filter is a CEL-expression used to decide whether this notification client should send the notification
	URL        string            `json:"url,omitempty"`                        // URL in the form of Shoutrrr notification service URL schema
	Template   string            `json:"template"`                             // Go template for the notification message
	Connection string            `json:"connection,omitempty"`                 // Connection is the name of the connection
	Properties map[string]string `json:"properties,omitempty" template:"true"` // Configuration properties for Shoutrrr. It's Templatable.
}

func (t NotificationConfig) Value() (driver.Value, error) {
	return types.GenericStructValue(t, true)
}

func (t *NotificationConfig) Scan(val any) error {
	return types.GenericStructScan(&t, val)
}

func (t NotificationConfig) GormDataType() string {
	return "notificationConfig"
}

func (t NotificationConfig) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	return types.JSONGormDBDataType(db.Dialector.Name())
}

func (t NotificationConfig) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return types.GormValue(t)
}

func (t *NotificationConfig) HydrateConnection(ctx *Context) error {
	connection, err := ctx.HydrateConnection(t.Connection)
	if err != nil {
		return err
	} else if connection == nil {
		return nil
	}

	t.URL = connection.URL

	return nil
}
