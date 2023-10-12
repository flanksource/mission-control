package jobs

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
)

var (
	ReconcilePageSize int

	// Only sync data created/updated in the last ReconcileMaxAge duration
	ReconcileMaxAge time.Duration
)

// SyncWithUpstream coordinates with upstream and pushes any resource
// that are missing on the upstream.
func SyncWithUpstream(ctx api.Context) error {
	logger.Debugf("running upstream reconcile job")

	jobHistory := models.NewJobHistory("SyncWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx, jobHistory.End())
	}()

	for _, table := range api.TablesToReconcile {
		if err := reconcileTable(ctx, table); err != nil {
			jobHistory.AddError(err.Error())
			logger.Errorf("failed to sync table %s: %v", table, err)
		} else {
			jobHistory.IncrSuccess()
		}
	}

	return nil
}

func reconcileTable(ctx api.Context, table string) error {
	newCtx, span := ctx.StartTrace("job-tracer", fmt.Sprintf("reconcile-%s", table))
	defer span.End()

	ctx = ctx.WithContext(newCtx)
	reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, ReconcilePageSize)

	return reconciler.SyncAfter(ctx, table, ReconcileMaxAge)
}

func SyncCheckStatuses(ctx api.Context) error {
	logger.Debugf("running check statuses sync job")

	jobHistory := models.NewJobHistory("SyncCheckStatusesWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx, jobHistory.End())
	}()

	if err := syncCheckStatuses(ctx); err != nil {
		logger.Errorf("failed to run checkstatus sync job: %v", err)
		jobHistory.AddError(err.Error())
		return err
	}

	jobHistory.IncrSuccess()
	return nil
}

// SyncCheckStatusesWithUpstream pushes new check statuses to upstream.
func syncCheckStatuses(ctx api.Context) error {
	var checkStatuses []models.CheckStatus
	if err := ctx.DB().Select("check_statuses.*").
		Joins("Left JOIN checks ON checks.id = check_statuses.check_id").
		Where("checks.agent_id = ?", uuid.Nil).
		Where("check_statuses.is_pushed IS FALSE").
		Find(&checkStatuses).Error; err != nil {
		return fmt.Errorf("failed to fetch checkstatuses: %w", err)
	}

	if len(checkStatuses) == 0 {
		return nil
	}

	logger.Debugf("Pushing %d check statuses to upstream in batches", len(checkStatuses))

	client := upstream.NewUpstreamClient(api.UpstreamConf)

	for i := 0; i < len(checkStatuses); i += ReconcilePageSize {
		end := i + ReconcilePageSize
		if end > len(checkStatuses) {
			end = len(checkStatuses)
		}
		batch := checkStatuses[i:end]

		logger.WithValues("batch", fmt.Sprintf("%d/%d", (i/ReconcilePageSize)+1, (len(checkStatuses)/ReconcilePageSize)+1)).
			Tracef("Pushing %d check statuses to upstream", len(batch))

		if err := client.Push(ctx, &upstream.PushData{AgentName: api.UpstreamConf.AgentName, CheckStatuses: batch}); err != nil {
			return fmt.Errorf("failed to push check statuses to upstream: %w", err)
		}

		for i := range batch {
			batch[i].IsPushed = true
		}

		if err := ctx.DB().Save(&batch).Error; err != nil {
			return fmt.Errorf("failed to save check statuses: %w", err)
		}
	}

	return nil
}
