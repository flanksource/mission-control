package actions

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/notification"
)

// Notification runs the notification action
type Notification struct {
}

func (t *Notification) Run(ctx context.Context, action v1.NotificationAction, env TemplateEnv) error {
	templater := gomplate.StructTemplater{
		RequiredTag: "template",
		DelimSets: []gomplate.Delims{
			{Left: "{{", Right: "}}"},
			{Left: "$(", Right: ")"},
		},
		ValueFunctions: true,
		Values:         env.AsMap(),
	}
	if err := templater.Walk(&action); err != nil {
		return fmt.Errorf("failed to walk template: %w", err)
	}

	notifContext := notification.NewContext(ctx, uuid.Nil)
	return notification.Send(notifContext, action.Connection, action.URL, action.Title, action.Message, action.Properties)
}
