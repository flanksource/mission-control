package jobs

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
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

	reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, ReconcilePageSize)
	for _, table := range api.TablesToReconcile {
		if err := reconciler.SyncAfter(ctx, table, ReconcileMaxAge); err != nil {
			jobHistory.AddError(err.Error())
			logger.Errorf("failed to sync table %s: %v", table, err)
		} else {
			jobHistory.IncrSuccess()
		}
	}

	return nil
}

// SyncCheckStatusesWithUpstream pushes new check statuses to upstream.
func SyncCheckStatusesWithUpstream(ctx api.Context) error {
	jobHistory := models.NewJobHistory("SyncCheckStatusesWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx, jobHistory.End())
	}()

	var checkStatuses []models.CheckStatus
	if err := ctx.DB().Where("NOW() - created_at <= INTERVAL '30 SECONDS'").Find(&checkStatuses).Error; err != nil {
		jobHistory.AddError(err.Error())
		return fmt.Errorf("failed to get check statuses: %w", err)
	}

	if len(checkStatuses) == 0 {
		return nil
	}

	logger.Tracef("Pushing %d check statuses to upstream", len(checkStatuses))
	if err := upstream.Push(ctx, api.UpstreamConf, &upstream.PushData{AgentName: api.UpstreamConf.AgentName, CheckStatuses: checkStatuses}); err != nil {
		jobHistory.AddError(err.Error())
		return fmt.Errorf("failed to push check_statuses to upstream: %w", err)
	}

	jobHistory.IncrSuccess()
	return nil
}
