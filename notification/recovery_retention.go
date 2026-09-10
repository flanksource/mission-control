package notification

import (
	"fmt"
	"strconv"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"gorm.io/gorm"
)

func recoveryPositiveInt(ctx context.Context, key string, fallback int) int {
	raw := ctx.Properties().String("notification.recovery."+key, strconv.Itoa(fallback))
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		ctx.Warnf("invalid notification.recovery.%s=%q; using %d", key, raw, fallback)
		return fallback
	}
	return value
}

func recoveryPositiveDuration(ctx context.Context, key string, fallback time.Duration) time.Duration {
	raw := ctx.Properties().String("notification.recovery."+key, fallback.String())
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		ctx.Warnf("invalid notification.recovery.%s=%q; using %s", key, raw, fallback)
		return fallback
	}
	return value
}

func recoveryRetentionDuration(ctx context.Context, key string, fallback time.Duration) time.Duration {
	raw := ctx.Properties().String("notification.recovery.retention."+key, fallback.String())
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		ctx.Warnf("invalid notification.recovery.retention.%s=%q; category disabled", key, raw)
		return 0
	}
	return value
}

func CleanupNotificationRecoveriesJob(ctx context.Context) *job.Job {
	return &job.Job{Name: "NotificationRecoveryRetention", Context: ctx, Schedule: "@every " + recoveryPositiveDuration(ctx, "retention.interval", time.Hour).String(), Singleton: false, JobHistory: true, Retention: job.RetentionFailed,
		Fn: func(runtime job.JobRuntime) error { return CleanupNotificationRecoveries(runtime.Context) }}
}

const recoveryRetentionLock int64 = 721463902

// CleanupNotificationRecoveries serializes bounded batches across processes, with a transaction-local lock and timeout.
func CleanupNotificationRecoveries(ctx context.Context) error {
	return cleanupNotificationRecoveries(ctx, time.Now())
}

func cleanupNotificationRecoveries(ctx context.Context, now time.Time) error {
	budget := recoveryPositiveDuration(ctx, "retention.budget", 5*time.Second)
	ctx, cancel := ctx.WithTimeout(budget)
	defer cancel()
	batch := recoveryPositiveInt(ctx, "retention.batch-size", 500)
	categories := []struct {
		name, table, age, predicate string
		retention                   time.Duration
	}{
		{"resolved", "notification_deliveries", "resolved_at", "resolved_at IS NOT NULL AND sent_at IS NOT NULL AND status = 'resolved' AND (lease_until IS NULL OR lease_until < NOW())", recoveryRetentionDuration(ctx, "resolved", 30*24*time.Hour)},
		{"exhausted", "notification_deliveries", "exhausted_at", "exhausted_at IS NOT NULL AND sent_at IS NOT NULL AND resolved_at IS NULL AND status = 'recovery-exhausted' AND (lease_until IS NULL OR lease_until < NOW())", recoveryRetentionDuration(ctx, "exhausted", 90*24*time.Hour)},
		{"episodes", "notification_health_episodes", "healthy_at", `healthy_at IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.episode_id = target.id)
   AND NOT EXISTS (SELECT 1 FROM notification_health_states s WHERE s.episode_id = target.id)`, recoveryRetentionDuration(ctx, "episodes", 30*24*time.Hour)},
	}
	grace := recoveryRetentionDuration(ctx, "deleted-states", 30*24*time.Hour)
	// Never shorten the conservative deletion-observation grace.
	if grace > 0 && grace < 30*24*time.Hour {
		ctx.Warnf("notification.recovery.retention.deleted-states clamped to 720h minimum")
		grace = 30 * 24 * time.Hour
	}
	counts := map[string]int64{}
	err := ctx.DB().Transaction(func(tx *gorm.DB) error {
		var locked bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?)", recoveryRetentionLock).Scan(&locked).Error; err != nil {
			return err
		}
		if !locked {
			return nil
		}
		if err := tx.Exec("SELECT set_config('statement_timeout', ?, true)", strconv.FormatInt(max(1, budget.Milliseconds()), 10)).Error; err != nil {
			return err
		}
		for _, category := range categories {
			if category.retention == 0 {
				continue
			}
			query := fmt.Sprintf(`WITH candidates AS (SELECT id FROM %s target WHERE %s AND %s < ? ORDER BY %s LIMIT ? FOR UPDATE SKIP LOCKED)
    DELETE FROM %s target USING candidates WHERE target.id = candidates.id`, category.table, category.predicate, category.age, category.age, category.table)
			result := tx.Exec(query, now.Add(-category.retention), batch)
			if result.Error != nil {
				return result.Error
			}
			counts[category.name] = result.RowsAffected
		}
		if grace == 0 {
			return nil
		}
		result := tx.Exec(`WITH candidates AS (
   SELECT resource_type, resource_id FROM notification_health_states s
   WHERE deletion_observed_at IS NOT NULL AND deletion_observed_at < ? AND health = 'deleted'
   AND ((resource_type = 'config' AND NOT EXISTS (SELECT 1 FROM config_items WHERE id = s.resource_id))
     OR (resource_type = 'component' AND NOT EXISTS (SELECT 1 FROM components WHERE id = s.resource_id))
     OR (resource_type = 'check' AND NOT EXISTS (SELECT 1 FROM checks WHERE id = s.resource_id)))
   AND NOT EXISTS (SELECT 1 FROM notification_health_episodes e WHERE e.resource_type = s.resource_type AND e.resource_id = s.resource_id
    AND (e.healthy_at IS NULL OR EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.episode_id = e.id)))
   AND NOT EXISTS (SELECT 1 FROM notification_health_episodes e WHERE e.id = s.episode_id
    AND (e.healthy_at IS NULL OR EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.episode_id = e.id)))
   ORDER BY deletion_observed_at LIMIT ? FOR UPDATE OF s SKIP LOCKED)
   DELETE FROM notification_health_states s USING candidates c WHERE s.resource_type = c.resource_type AND s.resource_id = c.resource_id`, now.Add(-grace), batch)
		counts["deleted-states"] = result.RowsAffected
		return result.Error
	})
	if err == nil {
		for category, count := range counts {
			ctx.Infof("notification recovery retention: %s deleted=%d", category, count)
		}
	}
	return err
}
