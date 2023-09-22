package upstream

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

var ReconcilePageSize int

// SyncWithUpstream coordinates with upstream and pushes any resource
// that are missing on the upstream.
func SyncWithUpstream(ctx api.Context) error {
	jobHistory := models.NewJobHistory("SyncWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx, jobHistory.End())
	}()

	// Only sync data created in the last 2 days
	const pastDuration = time.Hour * 48

	reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, ReconcilePageSize)
	for _, table := range api.TablesToReconcile {
		if err := reconciler.SyncAfter(ctx, table, pastDuration); err != nil {
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

	reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, ReconcilePageSize)
	if err := reconciler.SyncAfter(ctx, "check_statuses", time.Hour*30); err != nil {
		jobHistory.AddError(err.Error())
		return fmt.Errorf("failed to sync check_statuses table: %w", err)
	}

	jobHistory.IncrSuccess()
	return nil
}
