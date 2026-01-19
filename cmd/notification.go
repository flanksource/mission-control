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
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/notification"
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
	ConfigID   string
	Connection string

	// Common flags
	DryRun  bool
	Enqueue bool
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
	defer stop()
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

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
	if flags.ConfigID == "" {
		return fmt.Errorf("--config-id is required")
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

	configID, err := uuid.Parse(flags.ConfigID)
	if err != nil {
		return fmt.Errorf("invalid config-id: %w", err)
	}

	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", configID).First(&config).Error; err != nil {
		return fmt.Errorf("failed to find config with id %q: %w", configID, err)
	}

	event := models.Event{
		Name: flags.Event,
		Properties: map[string]string{
			"id":          configID.String(),
			"description": lo.FromPtr(config.Description),
			"status":      lo.FromPtr(config.Status),
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
		fmt.Printf("  Config ID: %s\n", flags.ConfigID)
		fmt.Printf("  Connection Override: %s\n", flags.Connection)
		fmt.Printf("  Title Template: %s\n", n.Title)
		fmt.Printf("  Message Template: %s\n", n.Template)

		celEnvMap := celEnv.AsMap(ctx)
		envJSON, err := json.MarshalIndent(celEnvMap, "  ", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode CEL environment: %w", err)
		}
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
		ID:             configID,
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

	if flags.Enqueue {
		return enqueueNotification(ctx, payload)
	}

	if err := notification.SendEventPayload(ctx, payload); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	fmt.Printf("Notification triggered successfully\n")
	return nil
}

func enqueueNotification(ctx context.Context, payload notification.NotificationEventPayload) error {
	event := models.Event{
		Name:       api.EventNotificationSend,
		Properties: payload.AsMap(),
	}
	if err := ctx.DB().Clauses(events.EventQueueOnConflictClause).Create(&event).Error; err != nil {
		return fmt.Errorf("failed to enqueue notification: %w", err)
	}
	fmt.Printf("Notification enqueued (event_id=%s) - will be processed by running instance\n", event.ID)
	return nil
}

func init() {
	NotificationSend.Flags().StringVar(&sendFlagsValues.ID, "id", "", "Notification ID to trigger")
	NotificationSend.Flags().StringVar(&sendFlagsValues.Name, "name", "", "Notification name to trigger (alternative to --id)")
	NotificationSend.Flags().StringVar(&sendFlagsValues.Event, "event", api.EventConfigUnhealthy, "Event name (e.g., config.unhealthy, check.failed)")
	NotificationSend.Flags().StringVar(&sendFlagsValues.ConfigID, "config-id", "", "Config item UUID")
	NotificationSend.Flags().StringVar(&sendFlagsValues.Connection, "connection", "", "Override the connection used to send the notification")

	NotificationSend.Flags().BoolVar(&sendFlagsValues.DryRun, "dry-run", false, "Preview without sending")
	NotificationSend.Flags().BoolVar(&sendFlagsValues.Enqueue, "enqueue", false, "Save to event queue for processing by a running instance (instead of sending directly)")

	Notification.AddCommand(NotificationSend)
	Root.AddCommand(Notification)
}
