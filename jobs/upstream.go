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
	newCtx, span := tracer.Start(ctx, fmt.Sprintf("reconcile-%s", table))
	defer span.End()

	ctx = ctx.WithContext(newCtx)
	reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, ReconcilePageSize)

	return reconciler.SyncAfter(ctx, table, ReconcileMaxAge)
}

// checkstatusSyncJob pushes new check statuses to upstream.
type checkstatusSyncJob struct {
	upstreamClient *upstream.UpstreamClient
	// created timestamp of the last check status that was pushed to upstream.
	lastCreated time.Time
}

func (t *checkstatusSyncJob) Run() {
	ctx := api.DefaultContext

	jobHistory := models.NewJobHistory("SyncCheckStatusesWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx, jobHistory.End())
	}()

	if err := t.run(ctx); err != nil {
		logger.Errorf("failed to run checkstatus sync job: %v", err)
		jobHistory.AddError(err.Error())
		return
	}

	jobHistory.IncrSuccess()
}

// SyncCheckStatusesWithUpstream pushes new check statuses to upstream.
func (t *checkstatusSyncJob) run(ctx api.Context) error {
	logger.Tracef("running checkstatus sync job since %s", t.lastCreated.Format(time.RFC3339))

	var checkStatuses []models.CheckStatus
	if t.lastCreated.IsZero() {
		if err := ctx.DB().Select("check_statuses.*").
			Joins("Left JOIN checks ON checks.id = check_statuses.check_id").
			Where("checks.agent_id = ?", uuid.Nil).
			Where("NOW() - check_statuses.created_at <= ?", ReconcileMaxAge).
			Order("check_statuses.created_at").
			Find(&checkStatuses).Error; err != nil {
			return fmt.Errorf("failed to fetch checkstatuses: %w", err)
		}
	} else {
		if err := ctx.DB().Select("check_statuses.*").
			Joins("Left JOIN checks ON checks.id = check_statuses.check_id").
			Where("checks.agent_id = ?", uuid.Nil).
			Where("check_statuses.created_at > ?", t.lastCreated).
			Order("check_statuses.created_at").
			Find(&checkStatuses).Error; err != nil {
			return fmt.Errorf("failed to fetch checkstatuses: %w", err)
		}
	}

	if len(checkStatuses) == 0 {
		return nil
	}

	for i := 0; i < len(checkStatuses); i += ReconcilePageSize {
		end := i + ReconcilePageSize
		if end > len(checkStatuses) {
			end = len(checkStatuses)
		}
		batch := checkStatuses[i:end]

		logger.WithValues("batch", fmt.Sprintf("%d/%d", (i/ReconcilePageSize)+1, (len(checkStatuses)/ReconcilePageSize)+1)).
			Tracef("Pushing %d check statuses to upstream", len(batch))

		if err := t.upstreamClient.Push(ctx, &upstream.PushData{AgentName: api.UpstreamConf.AgentName, CheckStatuses: batch}); err != nil {
			return fmt.Errorf("failed to push check statuses to upstream: %w", err)
		}

		t.lastCreated = batch[len(batch)-1].CreatedAt
	}

	return nil
}
