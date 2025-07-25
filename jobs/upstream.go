package jobs

import (
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/upstream"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/artifacts"
)

var (
	ReconcilePageSize int
)

func ReconcileAllJob(config upstream.UpstreamConfig) *job.Job {
	client := upstream.NewUpstreamClient(api.UpstreamConf)
	return &job.Job{
		Name:       "ReconcileAll",
		Schedule:   "@every 1m",
		Retention:  job.RetentionBalanced,
		Singleton:  true,
		JobHistory: true,
		RunNow:     true,
		Fn: func(ctx job.JobRuntime) error {
			ctx.History.ResourceType = job.ResourceTypeUpstream
			ctx.History.ResourceID = api.UpstreamConf.Host
			summary := upstream.ReconcileAll(ctx.Context, client, ReconcilePageSize)
			ctx.History.AddDetails("summary", summary)
			ctx.History.SuccessCount, ctx.History.ErrorCount = summary.GetSuccessFailure()
			if summary.Error() != nil {
				ctx.History.AddDetails("errors", summary.Error())
				ctx.History.ErrorCount += 1
			}

			return nil
		},
	}
}

// SyncArtifactRecords pushes any unpushed artifact records to the upstream.
// The actual artifacts aren't pushed by this job.
var SyncArtifactData = &job.Job{
	JobHistory: true,
	Singleton:  false, // this job is safe to run concurrently
	RunNow:     true,
	Retention:  job.RetentionFew,
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

// ResetIsPushed sets is_pushed field to false for all entities
// updated in the last 7 days
var ResetIsPushed = &job.Job{
	Name:       "ResetIsPushed",
	Schedule:   "15 3 * * *", // Everyday at 3:15 AM
	Retention:  job.RetentionFew,
	JobHistory: true,
	RunNow:     false,
	Singleton:  true,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypeUpstream
		ctx.History.ResourceID = api.UpstreamConf.Host
		return upstream.ResetIsPushed(ctx.Context)
	},
}
