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

	if err := upstream.SyncCheckStatuses(ctx, api.UpstreamConf, ReconcilePageSize); err != nil {
		logger.Errorf("failed to run checkstatus sync job: %v", err)
		jobHistory.AddError(err.Error())
		return err
	}

	jobHistory.IncrSuccess()
	return nil
}
