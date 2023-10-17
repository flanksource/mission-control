package jobs

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"go.opentelemetry.io/otel/trace"
)

var (
	ReconcilePageSize int

	// Only sync data created/updated in the last ReconcileMaxAge duration
	ReconcileMaxAge time.Duration
)

// SyncWithUpstream coordinates with upstream and pushes any resource
// that are missing on the upstream.
func SyncWithUpstream(ctx job.JobRuntime) error {
	logger.Debugf("running upstream reconcile job")

	jobHistory := models.NewJobHistory("SyncWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx.Context, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx.Context, jobHistory.End())
	}()

	for _, table := range api.TablesToReconcile {
		if err := reconcileTable(ctx.Context, table); err != nil {
			jobHistory.AddError(err.Error())
			logger.Errorf("failed to sync table %s: %v", table, err)
		} else {
			jobHistory.IncrSuccess()
		}
	}

	return nil
}

func reconcileTable(ctx context.Context, table string) error {
	//newCtx, span := ctx.StartTrace("job-tracer", fmt.Sprintf("reconcile-%s", table))
	var span trace.Span
	ctx, span = ctx.StartSpan(fmt.Sprintf("reconcile-%s", table))
	defer span.End()

	reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, ReconcilePageSize)

	return reconciler.SyncAfter(ctx, table, ReconcileMaxAge)
}

func SyncCheckStatuses(ctx job.JobRuntime) error {
	logger.Debugf("running check statuses sync job")

	jobHistory := models.NewJobHistory("SyncCheckStatusesWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx.Context, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx.Context, jobHistory.End())
	}()

	if err := upstream.SyncCheckStatuses(ctx.Context, api.UpstreamConf, ReconcilePageSize); err != nil {
		logger.Errorf("failed to run checkstatus sync job: %v", err)
		jobHistory.AddError(err.Error())
		return err
	}

	jobHistory.IncrSuccess()
	return nil
}
