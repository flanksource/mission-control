package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/notification"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var Notification = &cobra.Command{
	Use:   "notification",
	Short: "Manage notifications",
}

type sendFlags struct {
	// Trigger mode flags
	ID         string
	Name       string
	Event      string
	ResourceID string
	Connection string

	// Common flags
	DryRun bool
}

var sendFlagsValues sendFlags

var NotificationSend = &cobra.Command{
	Use:              "send",
	Short:            "Send a notification for debugging",
	PersistentPreRun: PreRun,
	RunE:             runNotificationSend,
}

func runNotificationSend(cmd *cobra.Command, args []string) error {
	logger.UseSlog()
	if err := properties.LoadFile("mission-control.properties"); err != nil {
		logger.Errorf(err.Error())
	}

	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)
	shutdown.WaitForSignal()

	if err := validateSendFlags(&sendFlagsValues); err != nil {
		return err
	}

	// Use system user for authorization
	sysUser, err := db.GetSystemUser(ctx)
	if err != nil {
		return fmt.Errorf("failed to get system user: %w", err)
	}
	ctx = ctx.WithUser(sysUser)

	return triggerExistingNotification(ctx, &sendFlagsValues)
}

func validateSendFlags(flags *sendFlags) error {
	if flags.ID == "" && flags.Name == "" {
		return fmt.Errorf("must specify either --id or --name")
	}
	if flags.ID != "" && flags.Name != "" {
		return fmt.Errorf("specify either --id or --name, not both")
	}
	if flags.ResourceID == "" {
		return fmt.Errorf("--resource-id is required")
	}

	return nil
}

func triggerExistingNotification(ctx context.Context, flags *sendFlags) error {
	notificationID := flags.ID
	if flags.Name != "" {
		var n models.Notification
		if err := ctx.DB().Where("name = ?", flags.Name).First(&n).Error; err != nil {
			return fmt.Errorf("failed to find notification with name %q: %w", flags.Name, err)
		}
		notificationID = n.ID.String()
	}

	resourceID, err := uuid.Parse(flags.ResourceID)
	if err != nil {
		return fmt.Errorf("invalid resource-id: %w", err)
	}

	event := models.Event{
		Name: flags.Event,
		Properties: map[string]string{
			"id": resourceID.String(),
		},
	}

	celEnv, err := notification.GetEnvForEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to get environment for event: %w", err)
	}

	n, err := notification.GetNotification(ctx, notificationID)
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	if flags.DryRun {
		fmt.Printf("Dry run mode - would trigger notification:\n")
		fmt.Printf("  Notification ID: %s\n", n.ID)
		fmt.Printf("  Notification Name: %s\n", n.Name)
		fmt.Printf("  Event: %s\n", flags.Event)
		fmt.Printf("  Resource ID: %s\n", flags.ResourceID)
		fmt.Printf("  Connection Override: %s\n", flags.Connection)
		fmt.Printf("  Title Template: %s\n", n.Title)
		fmt.Printf("  Message Template: %s\n", n.Template)

		celEnvMap := celEnv.AsMap(ctx)
		envJSON, _ := json.MarshalIndent(celEnvMap, "  ", "  ")
		fmt.Printf("  CEL Environment:\n  %s\n", string(envJSON))
		return nil
	}

	notifID, err := uuid.Parse(notificationID)
	if err != nil {
		return fmt.Errorf("invalid notification id: %w", err)
	}

	eventProps, err := json.Marshal(event.Properties)
	if err != nil {
		return fmt.Errorf("failed to encode event properties: %w", err)
	}

	payload := notification.NotificationEventPayload{
		ID:             resourceID,
		EventName:      flags.Event,
		NotificationID: notifID,
		EventCreatedAt: time.Now(),
		Properties:     eventProps,
	}

	switch {
	case flags.Connection != "":
		payload.CustomService = &api.NotificationConfig{Connection: flags.Connection}
	case len(n.CustomNotifications) > 0:
		payload.CustomService = &n.CustomNotifications[0]
	case n.PersonID != nil:
		payload.PersonID = n.PersonID
	case n.PlaybookID != nil:
		payload.PlaybookID = n.PlaybookID
	case n.TeamID != nil:
		return fmt.Errorf("notification uses a team recipient; use --connection to override for debug sending")
	default:
		return fmt.Errorf("notification has no supported recipient; use --connection to override for debug sending")
	}

	if err := notification.SendEventPayload(ctx, payload); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	fmt.Printf("Notification triggered successfully\n")
	return nil
}

func init() {
	NotificationSend.Flags().StringVar(&sendFlagsValues.ID, "id", "", "Notification ID to trigger")
	NotificationSend.Flags().StringVar(&sendFlagsValues.Name, "name", "", "Notification name to trigger (alternative to --id)")
	NotificationSend.Flags().StringVar(&sendFlagsValues.Event, "event", api.EventConfigUnhealthy, "Event name (e.g., config.unhealthy, check.failed)")
	NotificationSend.Flags().StringVar(&sendFlagsValues.ResourceID, "resource-id", "", "Resource UUID (config, check, or component)")
	NotificationSend.Flags().StringVar(&sendFlagsValues.Connection, "connection", "", "Override the connection used to send the notification")

	NotificationSend.Flags().BoolVar(&sendFlagsValues.DryRun, "dry-run", false, "Preview without sending")

	Notification.AddCommand(NotificationSend)
	Root.AddCommand(Notification)
}
