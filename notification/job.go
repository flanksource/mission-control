package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	CRDStatusUpdateQueue *collections.Queue[string]

	// Keeps track of all watchdog jobs
	watchdogJobs sync.Map
)

func InitCRDStatusUpdates(ctx context.Context) error {
	var err error
	CRDStatusUpdateQueue, err = collections.NewQueue(collections.QueueOpts[string]{
		Dedupe:     true,
		Equals:     func(a, b string) bool { return a == b },
		Comparator: func(a, b string) int { return 0 },
	})

	if err != nil {
		return err
	}

	go SyncCRDWatcher(ctx)
	return nil
}

func SyncCRDWatcher(ctx context.Context) {
	for {
		id, stop := CRDStatusUpdateQueue.Dequeue()
		if stop || id == "" {
			time.Sleep(30 * time.Second)
			continue
		}

		if err := SyncCRDStatus(ctx, id); err != nil {
			ctx.Errorf("error updating notification crd status: %v", err)
		}
	}

}

func SyncCRDStatusJob(ctx context.Context) *job.Job {
	return &job.Job{
		Name:       "SyncNotificationCRDStatus",
		Retention:  job.RetentionFailed,
		JobHistory: true,
		RunNow:     true,
		Context:    ctx,
		Singleton:  true,
		Schedule:   "@every 10m",
		Fn: func(ctx job.JobRuntime) error {
			return SyncCRDStatus(ctx.Context)
		},
	}
}

func SyncCRDStatus(ctx context.Context, ids ...string) error {
	if v1.NotificationReconciler.Client == nil {
		return errors.New("notification reconciler is not initialized")
	}

	summary, err := query.GetNotificationStats(ctx, ids...)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get notification stats")
	}

	for _, s := range summary {
		status := v1.NotificationStatus{
			Sent:       s.Sent,
			Pending:    s.Pending,
			Failed:     s.Failed,
			Error:      s.Error,
			LastSent:   metav1.Time{Time: s.UpdatedAt},
			LastFailed: metav1.Time{Time: s.LastFailedAt},
		}
		if err := patchCRDStatus(ctx, s.Name, s.Namespace, status); err != nil {
			return fmt.Errorf("error in patchCRDStatus: %w", err)
		}
	}

	return nil
}

func patchCRDStatus(ctx context.Context, name, namespace string, status v1.NotificationStatus) error {
	obj := &v1.Notification{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	rawPatch, err := json.Marshal(v1.Notification{Status: status})
	if err != nil {
		return fmt.Errorf("error marshaling status update for crd: %w", err)
	}
	patch := client.RawPatch(types.MergePatchType, rawPatch)
	if err := v1.NotificationReconciler.Status().Patch(ctx, obj, patch); err != nil {
		return fmt.Errorf("error patching crd status: %w", err)
	}
	return nil
}

func ProcessPendingNotificationsJob(ctx context.Context) *job.Job {
	return &job.Job{
		Name:       "ProcessPendingNotifications",
		Retention:  job.RetentionFew,
		JobHistory: true,
		RunNow:     true,
		Context:    ctx,
		Singleton:  false,
		Schedule:   "@every 15s",
		Fn: func(ctx job.JobRuntime) error {
			var iter int
			for {
				iter++
				if iter > 3 {
					// avoid getting stuck in a loop
					break
				}

				done, err := ProcessPendingNotifications(ctx.Context)
				if err != nil {
					ctx.History.AddErrorf("failed to send pending notification: %v", err)

					time.Sleep(2 * time.Second) // prevent spinning on db errors
					continue
				}

				ctx.History.IncrSuccess()
				if done {
					break
				}
			}

			return nil
		},
	}
}

func ProcessPendingNotifications(parentCtx context.Context) (bool, error) {
	var noMorePending bool

	err := parentCtx.DB().Transaction(func(tx *gorm.DB) error {
		ctx := parentCtx.WithDB(tx, parentCtx.Pool())

		var pending []models.NotificationSendHistory
		query := `
		SELECT *
		FROM notification_send_history
		WHERE status IN ?
			AND not_before <= NOW()
			AND (retries IS NULL OR retries < ?)
		ORDER BY not_before ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`

		statuses := []string{models.NotificationStatusEvaluatingWaitFor, models.NotificationStatusPending}
		maxRetries := ctx.Properties().Int("notification.max-retries", 4) - 1
		if err := ctx.DB().Raw(query, statuses, maxRetries).Find(&pending).Error; err != nil {
			return fmt.Errorf("failed to get pending notifications: %w", err)
		}

		if len(pending) == 0 {
			noMorePending = true
			return nil
		}

		currentHistory := pending[0]

		if err := processPendingNotification(ctx, currentHistory); err != nil {
			if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
				"status":  gorm.Expr("CASE WHEN retries >= ? THEN ? ELSE ? END", ctx.Properties().Int("notification.max-retries", 4)-1, models.NotificationStatusError, models.NotificationStatusPending),
				"error":   err.Error(),
				"retries": gorm.Expr("retries + 1"),
			}).Error; dberr != nil {
				return ctx.Oops().Join(dberr, err)
			}

			notif, notifErr := GetNotification(ctx, currentHistory.NotificationID.String())
			if notifErr != nil {
				return fmt.Errorf("failed to get notification: %w", notifErr)
			}

			if notif.HasFallbackSet() {
				// If the notification has fallback, we send to it after exhausting retries
				if err := models.GenerateFallbackAttempt(ctx.DB(), notif.Notification, currentHistory); err != nil {
					return fmt.Errorf("failed to generate fallback attempt: %w", err)
				}
			}
		}

		// we return nil or else the transaction will be rolled back and there'll be no trace of a failed attempt.
		return nil
	})

	return noMorePending, err
}

func ProcessFallbackNotificationsJob(ctx context.Context) *job.Job {
	return &job.Job{
		Name:       "ProcessFallbackNotifications",
		Retention:  job.RetentionFew,
		JobHistory: true,
		RunNow:     true,
		Context:    ctx,
		Singleton:  false,
		Schedule:   "@every 15s",
		Fn: func(ctx job.JobRuntime) error {
			var iter int
			for {
				iter++
				if iter > 3 {
					break
				}

				done, err := ProcessFallbackNotifications(ctx.Context)
				if err != nil {
					ctx.History.AddErrorf("failed to process notifications in status=%s : %v", models.NotificationStatusAttemptingFallback, err)
					time.Sleep(2 * time.Second) // prevent spinning on db errors
					continue
				}

				ctx.History.IncrSuccess()

				if done {
					break
				}
			}

			return nil
		},
	}
}

func ProcessFallbackNotifications(parentCtx context.Context) (bool, error) {
	var noMorePending bool

	err := parentCtx.DB().Transaction(func(tx *gorm.DB) error {
		ctx := parentCtx.WithDB(tx, parentCtx.Pool())

		var pending []models.NotificationSendHistory
		if err := ctx.DB().Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate, Options: clause.LockingOptionsSkipLocked}).
			Where("status = ?", models.NotificationStatusAttemptingFallback).
			Where("not_before <= NOW()").
			Limit(1). // one at a time; as one notification failure shouldn't affect a previous successful one
			Find(&pending).Error; err != nil {
			return fmt.Errorf("failed to get notifications to send to fallback: %w", err)
		}

		if len(pending) == 0 {
			noMorePending = true
			return nil
		}

		currentHistory := pending[0]
		ctx.Logger.V(6).Infof("attempting fallback notification (%s/%s) for resource %s",
			currentHistory.ID,
			currentHistory.Status,
			currentHistory.ResourceID,
		)

		if err := sendFallbackNotification(ctx, currentHistory); err != nil {
			if dberr := ctx.DB().Debug().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
				"status":  gorm.Expr("CASE WHEN retries >= ? THEN ? ELSE ? END", ctx.Properties().Int("notification.max-retries", 4)-1, models.NotificationStatusError, models.NotificationStatusAttemptingFallback),
				"error":   err.Error(),
				"retries": gorm.Expr("retries + 1"),
			}).Error; dberr != nil {
				return ctx.Oops().Join(dberr, err)
			}
		}

		// we return nil or else the transaction will be rolled back and there'll be no trace of a failed attempt.
		return nil
	})

	return noMorePending, err
}

// If the resource is still unhealthy, it returns false
func shouldSkipNotificationDueToHealth(ctx context.Context, notif NotificationWithSpec, currentHistory models.NotificationSendHistory) (bool, error) {
	var payload NotificationEventPayload
	payload.FromMap(currentHistory.Payload)

	originalEvent := models.Event{Name: payload.EventName, CreatedAt: payload.EventCreatedAt}
	if len(payload.Properties) > 0 {
		if err := json.Unmarshal(payload.Properties, &originalEvent.Properties); err != nil {
			return false, fmt.Errorf("failed to unmarshal properties: %w", err)
		}
	}

	celEnv, err := GetEnvForEvent(ctx, originalEvent)
	if err != nil {
		return false, fmt.Errorf("failed to get cel env: %w", err)
	}

	// check if the notification should go through an evaluation phase.
	// i.e. trigger an incremental scraper and re-process the notification.
	if celEnv.ConfigItem != nil && celEnv.ConfigItem.ID != uuid.Nil && currentHistory.Status != models.NotificationStatusEvaluatingWaitFor {
		if ok, err := isKubernetesConfigItem(ctx, celEnv.ConfigItem.ID.String()); err != nil {
			return false, fmt.Errorf("failed to check if the config belongs to a kubernetes scraper: %w", err)
		} else if ok {
			// for configs generated by a kubernetes scraper,
			// we trigger an incremental scrape and then queue the notification to be re-evaluate after 30s.
			ctx.Logger.V(6).Infof("adding notification to waitfor evaluation queue notification %s", currentHistory.ID)
			if err := triggerIncrementalScrape(ctx, celEnv.ConfigItem.ID.String()); err != nil {
				return false, fmt.Errorf("failed to trigger incremental scrape: %w", err)
			}

			if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
				"status":     models.NotificationStatusEvaluatingWaitFor,
				"not_before": gorm.Expr(fmt.Sprintf("not_before + INTERVAL '%f'", lo.CoalesceOrEmpty(lo.FromPtr(notif.WaitForEvalPeriod), time.Second*30).Seconds())),
			}).Error; dberr != nil {
				return false, dberr
			}

			return true, nil
		}
	}

	// previousHealth is the health that triggered the notification event
	previousHealth := api.EventToHealth(payload.EventName)

	resourceHealth, err := celEnv.GetResourceCurrentHealth(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get resource current health from cel env: %w", err)
	}
	currentHealth := resourceHealth.Health
	deleted := resourceHealth.DeletedAt != nil
	relativeUpdatedAt := time.Since(lo.FromPtr(resourceHealth.UpdatedAt))

	traceLog("NotificationID=%s HistoryID=%s Resource=[%s/%s] PreviousHealth=%s CurrentHealth=%s UpdatedAt=%s RelativeUpdatedAtAgo=%s Checking if reportable", notif.ID, currentHistory.ID, payload.EventName, payload.ID, previousHealth, currentHealth, lo.FromPtr(resourceHealth.UpdatedAt), relativeUpdatedAt)
	if !isHealthReportable(notif.Events, previousHealth, currentHealth) || deleted {
		ctx.Logger.V(6).Infof("skipping notification[%s] as health change is not reportable", notif.ID)
		traceLog("NotificationID=%s HistoryID=%s Resource=[%s/%s] PreviousHealth=%s CurrentHealth=%s ResourceDeleted=%v Skipping", notif.ID, currentHistory.ID, payload.EventName, payload.ID, previousHealth, currentHealth, deleted)

		if err := db.SkipNotificationSendHistory(ctx, currentHistory.ID); err != nil {
			return false, fmt.Errorf("failed to skip notification send history (%s): %w", currentHistory.ID, err)
		}

		return true, nil
	}
	traceLog("NotificationID=%s HistoryID=%s Resource=[%s/%s] PreviousHealth=%s CurrentHealth=%s Reporting ...", notif.ID, currentHistory.ID, payload.EventName, payload.ID, previousHealth, currentHealth)
	return false, nil
}

var traceLogger = logger.GetLogger("notification.debug.trace")

func traceLog(format string, args ...any) {
	if properties.On(false, "notification.tracing") {
		traceLogger.Infof(format, args...)
	}
}

func processPendingNotification(ctx context.Context, currentHistory models.NotificationSendHistory) error {
	notif, err := GetNotification(ctx, currentHistory.NotificationID.String())
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	// We need to re-evaluate the health of the resource
	// and ensure that the original event matches with the current health before we send out the notification.
	skipNotif, err := shouldSkipNotificationDueToHealth(ctx, *notif, currentHistory)
	if err != nil {
		return fmt.Errorf("failed to check if notification should be skipped: %w", err)
	}

	if skipNotif {
		return nil
	}

	var payload NotificationEventPayload
	payload.FromMap(currentHistory.Payload)

	event := models.Event{
		Name:      payload.EventName,
		CreatedAt: payload.EventCreatedAt,
		Properties: map[string]string{
			"id": payload.ID.String(),
		},
	}
	celEnv, err := GetEnvForEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to get cel env: %w", err)
	}

	silencedResource := getSilencedResourceFromCelEnv(celEnv)
	matchingSilences, err := db.GetMatchingNotificationSilences(ctx, silencedResource)
	if err != nil {
		return fmt.Errorf("failed to get matching silences: %w", err)
	}

	if blocker, err := processNotificationConstraints(ctx, *notif, payload, celEnv, matchingSilences); err != nil {
		return fmt.Errorf("failed to check all conditions for notification[%s]: %w", notif.ID, err)
	} else if blocker != nil {
		columns := map[string]any{
			"status": blocker.BlockedWithStatus,
		}

		if blocker.ParentID != nil {
			columns["parent_id"] = blocker.ParentID.String()
		}
		if blocker.SilencedBy != nil {
			columns["silenced_by"] = blocker.SilencedBy.String()
		}

		if err := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(columns).Error; err != nil {
			return fmt.Errorf("failed to save notification status as skipped: %w", err)
		}

		return nil
	}

	if err := sendPendingNotification(ctx, currentHistory, payload); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	} else if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
		"status": models.NotificationStatusSent,
	}).Error; dberr != nil {
		return fmt.Errorf("failed to save notification status as sent: %w", dberr)
	}

	return nil
}

func triggerIncrementalScrape(ctx context.Context, configID string) error {
	event := models.Event{
		Name: "config-db.incremental-scrape",
		Properties: map[string]string{
			"config_id": configID,
		},
	}

	onConflictClause := clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}, {Name: "properties"}},
		DoUpdates: clause.Assignments(map[string]any{
			"created_at": gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}
	return ctx.DB().Clauses(onConflictClause).Create(&event).Error
}

func isKubernetesConfigItem(ctx context.Context, configID string) (bool, error) {
	configItem, err := query.GetCachedConfig(ctx, configID)
	if err != nil {
		return false, fmt.Errorf("failed to get config(%s): %w", configID, err)
	}

	if lo.FromPtr(configItem.ScraperID) == "" {
		return false, nil
	}

	var scraper models.ConfigScraper
	if err := ctx.DB().Where("id = ?", lo.FromPtr(configItem.ScraperID)).Find(&scraper).Error; err != nil {
		return false, fmt.Errorf("failed to get config scraper(%s): %w", lo.FromPtr(configItem.ScraperID), err)
	}

	var scraperSpec struct {
		Kubernetes []map[string]any `json:"kubernetes"`
	}
	if err := json.Unmarshal([]byte(scraper.Spec), &scraperSpec); err != nil {
		return false, err
	}

	return len(scraperSpec.Kubernetes) != 0, nil
}

func InitWatchdogNotifications(ctx context.Context, scheduler *cron.Cron) error {
	var notifications []models.Notification
	if err := ctx.DB().
		Where("watchdog_interval IS NOT NULL AND watchdog_interval != 0").
		Where("deleted_at IS NULL").
		Find(&notifications).Error; err != nil {
		return fmt.Errorf("failed to get notifications with watchdog interval: %w", err)
	}

	ctx.Debugf("initializing watchdog jobs for %d notifications", len(notifications))
	for _, n := range notifications {
		_ = scheduleWatchdogJob(ctx, scheduler, n.ID.String(), n.WatchdogInterval.String())
	}

	return nil
}

func SyncWatchdogJob(ctx context.Context, scheduler *cron.Cron, notificationID string, interval *string) error {
	var scheduleChanged bool
	var existingJob *job.Job

	if j, ok := watchdogJobs.Load(notificationID); ok {
		existingJob = j.(*job.Job)
		if interval != nil {
			scheduleChanged = existingJob.Schedule != fmt.Sprintf("@every %s", *interval)
		}

		if interval == nil || scheduleChanged {
			ctx.Debugf("deleting existing watchdog job for %s", notificationID)
			existingJob.Unschedule()
			watchdogJobs.Delete(notificationID)
		}
	}

	if interval != nil && (scheduleChanged || existingJob == nil) {
		return scheduleWatchdogJob(ctx, scheduler, notificationID, *interval)
	}

	return nil
}

func scheduleWatchdogJob(ctx context.Context, scheduler *cron.Cron, notificationID string, interval string) error {
	ctx.Debugf("scheduling watchdog job for %s with interval %s", notificationID, interval)
	job := WatchdogNotificationJob(ctx, notificationID, interval)
	if err := job.AddToScheduler(scheduler); err != nil {
		return fmt.Errorf("failed to add watchdog job to scheduler: %w", err)
	}
	watchdogJobs.Store(notificationID, job)
	return nil
}

func WatchdogNotificationJob(ctx context.Context, notificationID string, interval string) *job.Job {
	schedule := fmt.Sprintf("@every %s", interval)
	return &job.Job{
		Name:          "NotificationWatchdog",
		Retention:     job.RetentionFailed,
		JitterDisable: false,
		ResourceID:    notificationID,
		ResourceType:  "notification",
		JobHistory:    true,
		RunNow:        false,
		Context:       ctx,
		Singleton:     true,
		Schedule:      schedule,
		Fn: func(ctx job.JobRuntime) error {
			return SendWatchdogNotification(ctx.Context.WithSubject(api.SystemUserID.String()), notificationID)
		},
	}
}

// SendWatchdogNotification sends a watchdog notification containing statistics
// for the specified notification
func SendWatchdogNotification(ctx context.Context, notificationID string) error {
	notificationUUID, err := uuid.Parse(notificationID)
	if err != nil {
		return fmt.Errorf("failed to parse notification ID: %w", err)
	}

	notification, err := GetNotification(ctx, notificationID)
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	if notification.PlaybookID == nil {
		// NOTE: Watchdog notifications aren't sent to playbook recievers.

		// Manually craft a payload (unlike other payloads that are generated from events)
		// This allows us to bypass the event queue and process synchronously
		payload := NotificationEventPayload{
			EventName:      api.EventWatchdog,
			EventCreatedAt: time.Now(),
			ID:             notificationUUID,
			PersonID:       notification.PersonID,
			NotificationID: notificationUUID,
			TeamID:         notification.TeamID,
			Properties:     fmt.Appendf(nil, `{"id": "%s"}`, notificationUUID.String()),
		}
		if len(notification.CustomNotifications) > 0 {
			payload.CustomService = &notification.CustomNotifications[0]
		}

		if err := sendNotification(ctx, payload); err != nil {
			return fmt.Errorf("failed to send watchdog notification to primary recipient: %w", err)
		}
	}

	if notification.FallbackPlaybookID == nil {
		// Also, send to fallback recipient
		payload := NotificationEventPayload{
			EventName:      api.EventWatchdog,
			EventCreatedAt: time.Now(),
			ID:             notificationUUID,
			NotificationID: notificationUUID,
			PersonID:       notification.FallbackPersonID,
			TeamID:         notification.FallbackTeamID,
			CustomService:  notification.FallbackCustomNotification,
			Properties:     fmt.Appendf(nil, `{"id": "%s"}`, notificationUUID.String()),
		}

		if err := sendNotification(ctx, payload); err != nil {
			return fmt.Errorf("failed to send watchdog notification to fallback recipient: %w", err)
		}
	}

	return nil
}
