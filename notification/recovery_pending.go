package notification

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// Opted-in pending sends use not_before as a bounded claim; external actions run after commit.
func processRecoveryPending(ctx context.Context, fallback bool) (bool, error) {
	statuses := []string{models.NotificationStatusPending, models.NotificationStatusEvaluatingWaitFor}
	if fallback {
		statuses = []string{models.NotificationStatusAttemptingFallback}
	}
	var rows []models.NotificationSendHistory
	err := ctx.DB().Raw(`WITH pending AS (
   SELECT h.* FROM notification_send_history h JOIN notifications n ON n.id = h.notification_id
   WHERE n.on_resolved->>'enabled' = 'true' AND h.status IN ? AND h.not_before <= NOW()
   ORDER BY h.not_before FOR UPDATE OF h SKIP LOCKED LIMIT 1
  ), claimed AS (
   UPDATE notification_send_history h SET not_before = NOW() + INTERVAL '5 minutes'
   FROM pending p WHERE h.id = p.id RETURNING h.id
  ) SELECT p.* FROM pending p JOIN claimed c ON p.id = c.id`, statuses).Scan(&rows).Error
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	history := rows[0]
	n, err := GetNotification(ctx, history.NotificationID.String())
	if err != nil {
		return true, err
	}
	if fallback {
		var payload NotificationEventPayload
		payload.FromMap(history.Payload)
		err = sendPendingNotification(ctx, history, payload)
	} else {
		err = processPendingNotification(ctx, history)
	}
	if err == nil {
		return true, nil
	}
	exhausted, dbErr := recordFailedSend(ctx, history, err, map[string]any{"not_before": time.Now().Add(time.Minute)})
	if dbErr != nil {
		return true, fmt.Errorf("pending recovery send failed: %v; persist: %w", err, dbErr)
	}
	if exhausted && !fallback && n.HasFallbackSet() {
		if err := materializeRecoveryFallback(ctx, n, history); err != nil {
			return true, err
		}
	}
	return true, err
}

func materializeRecoveryFallback(ctx context.Context, n *NotificationWithSpec, history models.NotificationSendHistory) error {
	var original NotificationEventPayload
	original.FromMap(history.Payload)
	event, err := original.originalEvent()
	if err != nil {
		return err
	}
	env, err := GetEnvForEvent(ctx, event)
	if err != nil {
		return err
	}
	fallback := *n
	fallback.PersonID, fallback.TeamID, fallback.PlaybookID = n.FallbackPersonID, n.FallbackTeamID, n.FallbackPlaybookID
	fallback.CustomNotifications = nil
	if n.FallbackCustomNotification != nil {
		fallback.CustomNotifications = append(fallback.CustomNotifications, *n.FallbackCustomNotification)
	}
	payloads, err := CreateNotificationSendPayloads(ctx.WithSubject(n.ID.String()), event, &fallback, env)
	if err != nil {
		return err
	}
	for _, payload := range payloads {
		payload.RecoveryEpisode = original.RecoveryEpisode
		id := uuid.NewSHA1(history.ID, []byte("fallback:"+payload.GenerateEventID().String()))
		notBefore := time.Now()
		if n.FallbackDelay != nil {
			notBefore = notBefore.Add(*n.FallbackDelay)
		}
		attempt := models.NotificationSendHistory{
			ID: id, NotificationID: n.ID, ParentID: &history.ID, ResourceID: history.ResourceID,
			SourceEvent: history.SourceEvent, Status: models.NotificationStatusAttemptingFallback,
			FirstObserved: history.FirstObserved, Payload: payload.AsMap(), NotBefore: &notBefore,
			PersonID: payload.PersonID, TeamID: payload.TeamID, ConnectionID: payload.Connection,
		}
		if err := ctx.DB().Clauses(clause.OnConflict{DoNothing: true}).Create(&attempt).Error; err != nil {
			return err
		}
	}
	return nil
}
