package jobs

import (
	"time"

	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/artifacts"
)

var (
	ReconcilePageSize int

	// Only sync data created/updated in the last ReconcileMaxAge duration
	ReconcileMaxAge time.Duration
)

// ReconcileTopologies pushes unpushed (or updated) topologies and components to the upstream
var ReconcileTopologies = &job.Job{
	Name:       "ReconcileTopologies",
	Schedule:   "@every 30m",
	Retention:  job.Retention3Day,
	JobHistory: true,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypeUpstream
		ctx.History.ResourceID = api.UpstreamConf.Host
		if count, err := upstream.ReconcileTable[models.Topology](ctx.Context, api.UpstreamConf, ReconcilePageSize); err != nil {
			ctx.History.AddError(err.Error())
		} else {
			ctx.History.SuccessCount += count
		}

		if count, err := upstream.ReconcileTable[models.Component](ctx.Context, api.UpstreamConf, ReconcilePageSize); err != nil {
			ctx.History.AddError(err.Error())
		} else {
			ctx.History.SuccessCount += count
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
		ctx.History.SuccessCount, err = upstream.ReconcileTable[models.Artifact](ctx.Context, api.UpstreamConf, ReconcilePageSize)
		if err != nil {
			ctx.History.AddError(err.Error())
		}
		return err
	},
}

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

		// We're using a custom batch size here because this job locks that many records while it's pushing it to the upstream.
		// It's run frequently and can run concurrently, so a small batch size is fine.
		batchSize := 10

		var err error
		ctx.History.SuccessCount, err = artifacts.SyncArtifactItems(ctx.Context, api.UpstreamConf, batchSize)
		if err != nil {
			ctx.History.AddError(err.Error())
		}

		return err
	},
}

// PingUpstream sends periodic heartbeat to the upstream
var PingUpstream = &job.Job{
	Name:       "PingUpstream",
	Schedule:   "@every 1m",
	Retention:  job.RetentionFailed,
	JobHistory: true,
	RunNow:     true,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypeUpstream
		ctx.History.ResourceID = api.UpstreamConf.Host
		client := upstream.NewUpstreamClient(api.UpstreamConf)
		return client.Ping(ctx.Context)
	},
}
