package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

type recoveryDispatch struct {
	EpisodeID    uuid.UUID
	Policy       v1.NotificationOnResolved
	ConnectionID *uuid.UUID
	SystemSMTP   bool
	Delivery     *models.NotificationDelivery
}

type recoveryDestination struct {
	SlackChannel string   `json:"slackChannel,omitempty"`
	To           []string `json:"to,omitempty"`
	From         string   `json:"from,omitempty"`
	FromName     string   `json:"fromName,omitempty"`
	Subject      string   `json:"subject,omitempty"`
}

func unresolvedHealth(health string) bool {
	return health == "warning" || health == "unhealthy" || health == "degraded"
}
func unresolvedEvent(event string) bool {
	switch event {
	case "config.warning", "config.unhealthy", "config.degraded", "component.warning", "component.unhealthy", "component.degraded", "check.failed":
		return true
	}
	return false
}

func currentRecoveryHealth(ctx context.Context, kind string, id uuid.UUID) (*models.NotificationHealthState, error) {
	if err := ctx.DB().Exec("SELECT refresh_notification_health(?, ?)", kind, id).Error; err != nil {
		return nil, err
	}
	var state models.NotificationHealthState
	if err := ctx.DB().Where("resource_type = ? AND resource_id = ?", kind, id).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func prepareRecoveryDispatch(ctx *Context, n *NotificationWithSpec, payload NotificationEventPayload) (bool, error) {
	if n.OnResolved == nil || !n.OnResolved.Enabled {
		return false, nil
	}
	ctx.recoveryPolicy = n.OnResolved
	if err := (v1.NotificationSpec{Events: n.Events, OnResolved: n.OnResolved}).ValidateOnResolved(); err != nil {
		return false, err
	}
	if len(n.GroupBy) > 0 || payload.GroupID != nil {
		return false, fmt.Errorf("onResolved does not support grouped notifications")
	}
	if payload.PlaybookID != nil {
		return false, fmt.Errorf("onResolved does not support playbook recipients")
	}
	if _, err := n.OnResolved.Delay(); err != nil {
		return false, err
	}
	if !unresolvedEvent(payload.EventName) {
		return false, nil
	}
	state, err := currentRecoveryHealth(ctx.Context, strings.SplitN(payload.EventName, ".", 2)[0], payload.ResourceID)
	if err != nil {
		return false, err
	}
	if !unresolvedHealth(state.Health) || state.EpisodeID == nil {
		return true, nil
	}
	if payload.RecoveryEpisode != state.EpisodeID.String() {
		return true, nil
	}
	ctx.recovery = &recoveryDispatch{EpisodeID: *state.EpisodeID, Policy: *n.OnResolved}
	return false, nil
}

// recoveryRoute binds retries to the persisted intent, before resolving credentials.
func recoveryRoute(ctx *Context, connectionName, transportURL string) (string, string, error) {
	if ctx.recovery == nil {
		return connectionName, transportURL, nil
	}
	var receipt models.NotificationDelivery
	err := ctx.DB().Where("history_id = ?", ctx.log.ID).First(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return connectionName, transportURL, nil
	}
	if err != nil {
		return "", "", err
	}
	ctx.recovery.Delivery = &receipt
	if receipt.ConnectionID != nil {
		return receipt.ConnectionID.String(), "", nil
	}
	if receipt.Transport != "smtp" {
		return "", "", fmt.Errorf("original delivery connection unavailable")
	}
	var destination recoveryDestination
	if err := json.Unmarshal(receipt.Destination, &destination); err != nil {
		return "", "", err
	}
	return "", "smtp://system/?To=" + url.QueryEscape(strings.Join(destination.To, ",")), nil
}

func setRecoveryTransport(ctx *Context, conn *models.Connection, transportURL string) error {
	policy := ctx.recoveryPolicy
	if policy == nil && ctx.recovery != nil {
		policy = &ctx.recovery.Policy
	}
	if policy == nil {
		return nil
	}
	if ctx.recovery != nil && ctx.recovery.Delivery != nil && conn != nil {
		expected := models.ConnectionTypeEmail
		if ctx.recovery.Delivery.Transport == "slack" {
			expected = models.ConnectionTypeSlack
		}
		if conn.Type != expected {
			return fmt.Errorf("original delivery connection changed type")
		}
	}
	if ctx.recovery != nil {
		ctx.recovery.ConnectionID = nil
		ctx.recovery.SystemSMTP = false
	}
	if conn != nil {
		switch conn.Type {
		case models.ConnectionTypeSlack:
			if err := policy.ValidateSlack(); err != nil {
				return err
			}
		case models.ConnectionTypeEmail:
		default:
			return fmt.Errorf("onResolved does not support connection type %q", conn.Type)
		}
		if ctx.recovery != nil {
			ctx.recovery.ConnectionID = &conn.ID
		}
	} else {
		if !strings.HasPrefix(transportURL, "smtp://system/") {
			return fmt.Errorf("onResolved requires native Slack, named SMTP or system SMTP")
		}
		if ctx.recovery != nil {
			ctx.recovery.SystemSMTP = true
		}
	}
	return nil
}

// beginDelivery durably records the intent before touching an external destination.
// A dispatching row after a crash is ambiguous, not proof of delivery or safe to resend.
func beginDelivery(ctx *Context, transport string, destination recoveryDestination) (*models.NotificationDelivery, error) {
	if ctx.recovery == nil {
		return nil, nil
	}
	b, err := json.Marshal(destination)
	if err != nil {
		return nil, err
	}
	policy, err := json.Marshal(ctx.recovery.Policy)
	if err != nil {
		return nil, err
	}
	id := uuid.New()
	receipt := models.NotificationDelivery{
		ID: id, NotificationID: ctx.notificationID, EpisodeID: ctx.recovery.EpisodeID,
		HistoryID: &ctx.log.ID, ConnectionID: ctx.recovery.ConnectionID,
		Transport: transport, Destination: types.JSON(b), Policy: types.JSON(policy),
		MessageID: fmt.Sprintf("<%s@notifications.mission-control>", id), Status: "prepared",
		CreatedAt: time.Now(), NotBefore: time.Now(),
	}
	if err := ctx.DB().Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "history_id"}}, DoNothing: true}).Create(&receipt).Error; err != nil {
		return nil, err
	}
	receipt = models.NotificationDelivery{}
	if err := ctx.DB().Where("history_id = ?", ctx.log.ID).First(&receipt).Error; err != nil {
		return nil, err
	}
	if receipt.Transport != transport || (receipt.ConnectionID == nil) != (ctx.recovery.ConnectionID == nil) ||
		(receipt.ConnectionID != nil && *receipt.ConnectionID != *ctx.recovery.ConnectionID) {
		return nil, fmt.Errorf("delivery intent transport or connection mismatch")
	}
	if receipt.SentAt != nil {
		ctx.recovery.Delivery = &receipt
		return &receipt, nil
	}
	if receipt.Status == "dispatching" {
		return nil, fmt.Errorf("delivery %s is ambiguous after interrupted dispatch; inspect destination before retrying", receipt.ID)
	}
	result := ctx.DB().Model(&receipt).Where("status IN ('prepared', 'error')").Updates(map[string]any{"status": "dispatching", "error": nil})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("delivery %s already claimed", receipt.ID)
	}
	ctx.recovery.Delivery = &receipt
	return &receipt, nil
}

func finishDelivery(ctx *Context, receipt *models.NotificationDelivery, channel, messageID string, sendErr error) error {
	if receipt == nil {
		return sendErr
	}
	values := map[string]any{"status": "sent", "sent_at": time.Now(), "error": nil}
	if channel != "" {
		values["channel"] = channel
		values["message_id"] = messageID
	}
	if sendErr != nil {
		values = map[string]any{"status": "error", "error": "original transport dispatch failed; see send history"}
	}
	if err := ctx.DB().Model(receipt).Updates(values).Error; err != nil {
		// Channel/timestamp are non-secret, and essential to repair an external-success/DB-failure ambiguity.
		ctx.Errorf("notification receipt persistence failed: delivery=%s channel=%s message_id=%s external_success=%t error=%v", receipt.ID, channel, messageID, sendErr == nil, err)
		return errors.Join(sendErr, fmt.Errorf("delivery %s receipt persistence failed (external success=%t): %w", receipt.ID, sendErr == nil, err))
	}
	if sendErr == nil {
		if err := ReconcileNotificationRecoveries(ctx.Context); err != nil {
			ctx.Errorf("post-send recovery reconciliation: %v", err)
		}
	}
	return sendErr
}

func recoveryBody(episode models.NotificationHealthEpisode, state models.NotificationHealthState, now time.Time) string {
	ended := now
	if episode.HealthyAt != nil {
		ended = *episode.HealthyAt
	}
	return fmt.Sprintf("Resolved: %s/%s is healthy. Recovery time: %s. Outage duration: %s. Notification time: %s.", episode.ResourceType, episode.ResourceID, ended.UTC().Format(time.RFC3339), ended.Sub(episode.StartedAt).Round(time.Second), now.UTC().Format(time.RFC3339))
}

var errRecoveryDeferred = errors.New("recovery deferred until resource is stably healthy")
