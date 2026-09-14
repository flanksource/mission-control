package notification

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type recoveryDeferral struct{ deadline time.Time }

func (e *recoveryDeferral) Error() string        { return errRecoveryDeferred.Error() }
func (e *recoveryDeferral) Is(target error) bool { return target == errRecoveryDeferred }

func laterRecoveryDeadline(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

const wakeableRecoveryPredicate = "sent_at IS NOT NULL AND resolved_at IS NULL AND status IN ('sent', 'waiting-for-healthy')"

// lockRecoveryHealth serializes wake-marker decisions before receipt mutations.
// Receipt inspection must use a subsequent statement, after this lock is acquired.
func lockRecoveryHealth(tx *gorm.DB, kind string, id uuid.UUID) (*models.NotificationHealthState, error) {
	var state models.NotificationHealthState
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("resource_type = ? AND resource_id = ?", kind, id).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

// scheduleRecovery only changes health scheduling, never failure budget, backoff or operation checkpoints.
// The caller holds the resource's health-state lock before mutating the receipt.
func scheduleRecovery(tx *gorm.DB, receipt models.NotificationDelivery, state *models.NotificationHealthState) error {
	values := map[string]any{"status": "waiting-for-healthy"}
	if state.Health == "healthy" && state.HealthySince != nil {
		var policy v1.NotificationOnResolved
		deadline := time.Now()
		if err := json.Unmarshal(receipt.Policy, &policy); err == nil {
			if delay, err := policy.Delay(); err == nil {
				deadline = state.HealthySince.Add(delay)
			}
		}
		// Invalid saved policies are sent to the regular bounded failure path.
		values["status"] = "stabilizing"
		values["not_before"] = laterRecoveryDeadline(receipt.NotBefore, deadline)
	}
	if receipt.Status == values["status"] {
		return nil
	}
	return tx.Model(&models.NotificationDelivery{}).Where("id = ? AND "+wakeableRecoveryPredicate+" AND (lease_until IS NULL OR lease_until < NOW())", receipt.ID).Updates(values).Error
}

func wakeNotificationDelivery(ctx context.Context, id uuid.UUID) error {
	ctx, cancel := ctx.WithTimeout(5 * time.Second)
	defer cancel()
	var receipt models.NotificationDelivery
	if err := ctx.DB().Where("id = ? AND "+wakeableRecoveryPredicate, id).First(&receipt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var episode models.NotificationHealthEpisode
	if err := ctx.DB().Where("id = ?", receipt.EpisodeID).First(&episode).Error; err != nil {
		return err
	}
	if _, err := currentRecoveryHealth(ctx, episode.ResourceType, episode.ResourceID); err != nil {
		return err
	}
	return ctx.DB().Transaction(func(tx *gorm.DB) error {
		state, err := lockRecoveryHealth(tx, episode.ResourceType, episode.ResourceID)
		if err != nil {
			return err
		}
		return scheduleRecovery(tx, receipt, state)
	})
}

// wakeNotificationResource queues a bounded resource-local batch; it never calls a transport.
func wakeNotificationResource(ctx context.Context, kind string, id uuid.UUID) error {
	ctx, cancel := ctx.WithTimeout(5 * time.Second)
	defer cancel()
	if _, err := currentRecoveryHealth(ctx, kind, id); err != nil {
		return err
	}
	return wakeNotificationResourceBatch(ctx, kind, id)
}

func wakeNotificationResourceBatch(ctx context.Context, kind string, id uuid.UUID) error {
	return ctx.DB().Transaction(func(tx *gorm.DB) error {
		state, err := lockRecoveryHealth(tx, kind, id)
		if err != nil {
			return err
		}
		var receipts []models.NotificationDelivery
		if err := tx.Raw(`SELECT d.* FROM notification_health_episodes e JOIN notification_deliveries d ON d.episode_id = e.id
   WHERE e.resource_type = ? AND e.resource_id = ? AND d.sent_at IS NOT NULL AND d.resolved_at IS NULL
   AND d.status IN ('sent', 'waiting-for-healthy') AND (d.lease_until IS NULL OR d.lease_until < NOW())
   ORDER BY e.id, d.id LIMIT ? FOR UPDATE OF d SKIP LOCKED`, kind, id, recoveryPositiveInt(ctx, "wake.batch-size", 20)).Scan(&receipts).Error; err != nil {
			return err
		}
		for _, receipt := range receipts {
			if err := scheduleRecovery(tx, receipt, state); err != nil {
				return err
			}
		}
		return tx.Exec(`UPDATE notification_health_states SET wake_pending = false
   WHERE resource_type = ? AND resource_id = ? AND generation = ? AND wake_pending
   AND NOT EXISTS (SELECT 1 FROM notification_health_episodes e JOIN notification_deliveries d ON d.episode_id = e.id
    WHERE e.resource_type = ? AND e.resource_id = ? AND d.sent_at IS NOT NULL AND d.resolved_at IS NULL
    AND d.status IN ('sent', 'waiting-for-healthy'))`, kind, id, state.Generation, kind, id).Error
	})
}

func SweepNotificationRecoveriesJob(ctx context.Context) *job.Job {
	return &job.Job{Name: "NotificationRecoverySweep", Context: ctx, Schedule: "@every " + recoveryPositiveDuration(ctx, "sweep.interval", 5*time.Minute).String(), RunNow: true, Singleton: false, JobHistory: true, Retention: job.RetentionFailed,
		Fn: func(runtime job.JobRuntime) error { return SweepNotificationRecoveries(runtime.Context) }}
}

// SweepNotificationRecoveries consumes durable healthy wake markers, including missed/coalesced events.
func SweepNotificationRecoveries(ctx context.Context) error {
	ctx, cancel := ctx.WithTimeout(recoveryPositiveDuration(ctx, "sweep.budget", 5*time.Second))
	defer cancel()
	var states []models.NotificationHealthState
	if err := ctx.DB().Where("health = 'healthy' AND wake_pending").Order("resource_type, resource_id").Limit(recoveryPositiveInt(ctx, "sweep.batch-size", 100)).Find(&states).Error; err != nil {
		return err
	}
	var failures []error
	for _, state := range states {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		if err := wakeNotificationResource(ctx, state.ResourceType, state.ResourceID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
