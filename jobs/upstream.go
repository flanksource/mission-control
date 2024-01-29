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
		ctx.History.ResourceType = job.ResourceTypeUpstream
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

var SyncCheckStatuses = &job.Job{
	JobHistory: true,
	Singleton:  true,
	Retention:  job.RetentionHour,
	Name:       "SyncCheckStatuses",
	Schedule:   "@every 30s",
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypeUpstream
		ctx.History.ResourceID = api.UpstreamConf.Host
		var err error
		if ctx.History.SuccessCount, err = upstream.SyncCheckStatuses(ctx.Context, api.UpstreamConf, ReconcilePageSize); err != nil {
			ctx.History.AddError(err.Error())
		}
		return nil
	},
}

// SyncArtifactRecords pushes any unpushed artifact records to the upstream.
// The actual artifacts aren't pushed by this job.
var SyncArtifactRecords = &job.Job{
	JobHistory: true,
	Singleton:  true,
	Retention:  job.RetentionHour,
	Name:       "SyncArtifactRecords",
	Schedule:   "@every 30s",
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypeUpstream
		ctx.History.ResourceID = api.UpstreamConf.Host
		var err error
		ctx.History.SuccessCount, err = upstream.SyncArtifacts(ctx.Context, api.UpstreamConf, ReconcilePageSize)
		if err != nil {
			ctx.History.AddError(err.Error())
		}
		return err
	},
}

// agentArtifactPath is the local path to the agent artifact store.
var agentArtifactPath string

// SyncArtifactRecords pushes any unpushed artifact records to the upstream.
// The actual artifacts aren't pushed by this job.
var SyncArtifactData = &job.Job{
	JobHistory: true,
	Singleton:  false, // this job is safe to run concurrently
	RunNow:     true,
	Retention:  job.RetentionHour,
	Name:       "SyncArtifactData",
	Schedule:   "@every 30s",
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypeUpstream
		ctx.History.ResourceID = api.UpstreamConf.Host

		if agentArtifactPath == "" {
			artifactConnection, err := ctx.HydrateConnectionByURL(api.DefaultArtifactConnection)
			if err != nil {
				return err
			} else if artifactConnection == nil {
				return fmt.Errorf("artifact connection (%s) not found", api.DefaultArtifactConnection)
			}

			if val, ok := artifactConnection.Properties["path"]; !ok {
				return fmt.Errorf("artifact connection is invalid. path not set")
			} else {
				agentArtifactPath = val
			}
		}

		var err error
		ctx.History.SuccessCount, err = upstream.SyncArtifactItems(ctx.Context, api.UpstreamConf, agentArtifactPath, ReconcilePageSize)
		if err != nil {
			ctx.History.AddError(err.Error())
		}
		return err
	},
}
