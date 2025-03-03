package api

import (
	gocontext "context"
	"database/sql/driver"

	"github.com/flanksource/duty/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// SystemSMTP indicates that the shoutrrr URL for smtp should use
// the system's SMTP credentials.
const SystemSMTP = "smtp://system/"

// +kubebuilder:object:generate=true
type NotificationConfig struct {
	Name       string            `json:"name,omitempty"`                       // A unique name to identify this notification configuration.
	Filter     string            `json:"filter,omitempty"`                     // Filter is a CEL-expression used to decide whether this notification client should send the notification
	URL        string            `json:"url,omitempty"`                        // URL in the form of Shoutrrr notification service URL schema
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

func (t NotificationConfig) GormValue(ctx gocontext.Context, db *gorm.DB) clause.Expr {
	return types.GormValue(t)
}
