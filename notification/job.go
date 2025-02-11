package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/hints"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var CRDStatusUpdateQueue *collections.Queue[string]

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

	var summary []struct {
		Name      string
		Namespace string
		Sent      int
		Failed    int
		Pending   int
		UpdatedAt time.Time
		Error     string
	}

	q := ctx.DB().Clauses(hints.CommentBefore("select", "notification_crd_sync")).
		Table("notifications_summary").
		Where("name != '' AND namespace != '' AND source = ?", models.SourceCRD)

	if len(ids) > 0 {
		q = q.Where("id in ?", ids)
	}
	if err := q.Find(&summary).Error; err != nil {
		return fmt.Errorf("error querying notifications_summary: %w", err)
	}

	for _, s := range summary {
		status := v1.NotificationStatus{
			Sent:     s.Sent,
			Pending:  s.Pending,
			Failed:   s.Failed,
			Error:    s.Error,
			LastSent: metav1.Time{Time: s.UpdatedAt},
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
		Retention:  job.RetentionFailed,
		JobHistory: true,
		RunNow:     true,
		Context:    ctx,
		Singleton:  false,
		Schedule:   "@every 30s",
		Fn: func(ctx job.JobRuntime) error {
			var errCount int
			for {
				done, err := ProcessPendingNotifications(ctx.Context)
				if err != nil {
					ctx.History.AddErrorf("failed to send pending notification: %v", err)

					errCount++
					if errCount > 3 {
						// avoid getting stuck in a loop
						break
					}

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
		if err := ctx.DB().Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate, Options: clause.LockingOptionsSkipLocked}).
			Where("status IN (?, ?)", models.NotificationStatusPending, models.NotificationStatusEvaluatingWaitFor).
			Where("not_before <= NOW()").
			Where("retries < ? ", ctx.Properties().Int("notification.max-retries", 4)).
			Where("group_by_hash = ''").
			Order("not_before").
			Limit(1). // one at a time; as one notification failure shouldn't affect a previous successful one
			Find(&pending).Error; err != nil {
			return fmt.Errorf("failed to get pending notifications: %w", err)
		}

		if len(pending) == 0 {
			noMorePending = true
			return nil
		}

		currentHistory := pending[0]
		ctx.Logger.V(6).Infof("processing notification (%s/%s) for resource %s",
			currentHistory.ID,
			currentHistory.Status,
			currentHistory.ResourceID,
		)

		if err := processPendingNotification(ctx, currentHistory, []models.NotificationSendHistory{}); err != nil {
			if dberr := ctx.DB().Debug().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
				"status":  gorm.Expr("CASE WHEN retries >= ? THEN ? ELSE ? END", ctx.Properties().Int("notification.max-retries", 4)-1, models.NotificationStatusError, models.NotificationStatusPending),
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

func ProcessPendingGroupedNotificationsJob(ctx context.Context) *job.Job {
	return &job.Job{
		Name:       "ProcessPendingGroupedNotifications",
		Retention:  job.RetentionFailed,
		JobHistory: true,
		RunNow:     true,
		Context:    ctx,
		Singleton:  true,
		Schedule:   "@every 30s",
		Fn: func(ctx job.JobRuntime) error {
			return ProcessPendingGroupedNotifications(ctx.Context)
		},
	}
}

func ProcessPendingGroupedNotifications(parentCtx context.Context) error {
	var rows []struct {
		GroupByHash       string
		EarliestNotBefore time.Time
	}
	if err := parentCtx.DB().Model(&models.NotificationSendHistory{}).
		Select("group_by_hash, MIN(not_before) AS earliest_not_before").
		Group("group_by_hash").
		Where("status IN (?, ?)", models.NotificationStatusPending, models.NotificationStatusEvaluatingWaitFor).
		Where("not_before <= NOW()").
		Where("retries < ? ", parentCtx.Properties().Int("notification.max-retries", 4)).
		Where("group_by_hash != ''").
		Order("earliest_not_before").
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("error fetching group_by_hash: %w", err)
	}

	for _, r := range rows {
		err := parentCtx.DB().Transaction(func(tx *gorm.DB) error {
			ctx := parentCtx.WithDB(tx, parentCtx.Pool())

			var pending []models.NotificationSendHistory
			if err := ctx.DB().Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate, Options: clause.LockingOptionsSkipLocked}).
				Where("status IN (?, ?)", models.NotificationStatusPending, models.NotificationStatusEvaluatingWaitFor).
				Where("not_before <= NOW()").
				Where("group_by_hash = ?", r.GroupByHash).
				Find(&pending).Error; err != nil {
				return fmt.Errorf("failed to get pending notifications: %w", err)
			}

			if len(pending) == 0 {
				return nil
			}

			currentHistory := pending[0]
			ctx.Logger.V(6).Infof("processing notification (%s/%s) for resource %s",
				currentHistory.ID,
				currentHistory.Status,
				currentHistory.ResourceID,
			)

			var groupedHistory []models.NotificationSendHistory
			if len(pending) > 1 {
				groupedHistory = pending[1:]

			}

			historyIDs := lo.Map(pending, func(h models.NotificationSendHistory, _ int) uuid.UUID { return h.ID })
			if err := processPendingNotification(ctx, currentHistory, groupedHistory); err != nil {
				if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id IN ?", historyIDs).UpdateColumns(map[string]any{
					"status":  gorm.Expr("CASE WHEN retries >= ? THEN ? ELSE ? END", ctx.Properties().Int("notification.max-retries", 4)-1, models.NotificationStatusError, models.NotificationStatusPending),
					"error":   err.Error(),
					"retries": gorm.Expr("retries + 1"),
				}).Error; dberr != nil {
					return ctx.Oops().Join(dberr, err)
				}
			}

			// we return nil or else the transaction will be rolled back and there'll be no trace of a failed attempt.
			return nil
		})

		if err != nil {
			return fmt.Errorf("error processing grouped notification transaction: %w", err)
		}
	}

	return nil
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

	currentHealth, deleted, err := celEnv.GetResourceHealth(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get resource health from cel env: %w", err)
	}

	traceLog("NotificationID=%s HistoryID=%s Resource=[%s/%s] PreviousHealth=%s CurrentHealth=%s Checking if reportable", notif.ID, currentHistory.ID, payload.EventName, payload.ID, previousHealth, currentHealth)
	if !isHealthReportable(notif.Events, previousHealth, currentHealth) || deleted {
		ctx.Logger.V(6).Infof("skipping notification[%s] as health change is not reportable", notif.ID)
		traceLog("NotificationID=%s HistoryID=%s Resource=[%s/%s] PreviousHealth=%s CurrentHealth=%s ResourceDeleted=%v Skipping", notif.ID, currentHistory.ID, payload.EventName, payload.ID, previousHealth, currentHealth, deleted)
		if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", currentHistory.ID).UpdateColumns(map[string]any{
			"status": models.NotificationStatusSkipped,
		}).Error; dberr != nil {
			return false, fmt.Errorf("failed to save notification status as skipped: %w", dberr)
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

func processPendingNotification(ctx context.Context, currentHistory models.NotificationSendHistory, groupedHistory []models.NotificationSendHistory) error {
	notif, err := GetNotification(ctx, currentHistory.NotificationID.String())
	if err != nil {
		return fmt.Errorf("failed to get notification: %w", err)
	}

	var historiesToUpdate []models.NotificationSendHistory

	// We need to re-evaluate the health of the resource
	// and ensure that the original event matches with the current health before we send out the notification.
	skipNotif, err := shouldSkipNotificationDueToHealth(ctx, *notif, currentHistory)
	if err != nil {
		return err
	}
	if !skipNotif {
		historiesToUpdate = append(historiesToUpdate, currentHistory)
	}
	for _, h := range groupedHistory {
		skipNotif, err := shouldSkipNotificationDueToHealth(ctx, *notif, h)
		if err != nil {
			return err
		}
		if !skipNotif {
			historiesToUpdate = append(historiesToUpdate, h)
		}
	}

	if len(historiesToUpdate) == 0 {
		return nil
	}

	var payload NotificationEventPayload
	historyToUpdate := historiesToUpdate[0]
	payload.FromMap(historyToUpdate.Payload)
	if len(historiesToUpdate) > 1 {
		payload.GroupedResources = lo.Map(historiesToUpdate[1:], func(h models.NotificationSendHistory, _ int) string {
			if strings.HasPrefix(h.SourceEvent, "config") {
				ci, _ := query.GetCachedConfig(ctx, h.ResourceID.String())
				return strings.Join([]string{ci.ID.String(), lo.FromPtr(ci.Type), lo.FromPtr(ci.Name)}, "/")
			}
			if strings.HasPrefix(h.SourceEvent, "component") {
				comp, _ := query.GetCachedComponent(ctx, h.ResourceID.String())
				return strings.Join([]string{comp.ID.String(), comp.Type, comp.Name}, "/")
			}
			if strings.HasPrefix(h.SourceEvent, "check") {
				check, _ := query.FindCachedCheck(ctx, h.ResourceID.String())
				if check != nil {
					return strings.Join([]string{check.ID.String(), check.Type, check.Name}, "/")
				}
			}
			return ""
		})

	}
	historyIDs := lo.Map(historiesToUpdate, func(h models.NotificationSendHistory, _ int) uuid.UUID { return h.ID })

	if err := sendPendingNotification(ctx, historyToUpdate, payload); err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	} else if dberr := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id IN ?", historyIDs).UpdateColumns(map[string]any{
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
