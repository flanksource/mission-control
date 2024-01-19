package jobs

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"go.opentelemetry.io/otel/trace"
)

var (
	ReconcilePageSize int

	// Only sync data created/updated in the last ReconcileMaxAge duration
	ReconcileMaxAge time.Duration
)

// SyncWithUpstream coordinates with upstream and pushes any resource
// that are missing on the upstream.
var SyncWithUpstream = &job.Job{
	Name:       "SyncWithUpstream",
	Schedule:   "@every 8h",
	Retention:  job.Retention3Day,
	JobHistory: true,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = "upstream"
		ctx.History.ResourceID = api.UpstreamConf.Host
		for _, table := range api.TablesToReconcile {
			if count, err := reconcileTable(ctx.Context, table); err != nil {
				ctx.History.AddError(err.Error())
			} else {
				ctx.History.SuccessCount += count
			}
		}
		return nil
	},
}

func reconcileTable(ctx context.Context, table string) (int, error) {
	var span trace.Span
	ctx, span = ctx.StartSpan(fmt.Sprintf("reconcile-%s", table))
	defer span.End()
	reconciler := upstream.NewUpstreamReconciler(api.UpstreamConf, ReconcilePageSize)

	count, err := reconciler.SyncAfter(ctx, table, ReconcileMaxAge)
	if err != nil {
		return count, err
	}
	ctx.Tracef("upstream reconcile synced %d resources for %s", count, table)
	return count, err
}

var UpstreamJobs = []*job.Job{
	SyncWithUpstream,
	SyncCheckStatuses,
	PushPlaybookActions,
	PullPlaybookActions,
}

var SyncCheckStatuses = &job.Job{
	JobHistory: true,
	Singleton:  true,
	Retention:  job.RetentionHour,
	Name:       "SyncCheckStatuses",
	Schedule:   "@every 30s",
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = "upstream"
		ctx.History.ResourceID = api.UpstreamConf.Host
		var err error
		if ctx.History.SuccessCount, err = upstream.SyncCheckStatuses(ctx.Context, api.UpstreamConf, ReconcilePageSize); err != nil {
			ctx.History.AddError(err.Error())
		}
		return nil
	},
}
