package jobs

import (
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"

	"github.com/flanksource/incident-commander/playbook"
)

// PullPlaybookActions periodically pulls playbook actions to run
// from the upstream
var PullPlaybookActions = &job.Job{
	Name:       "PullPlaybookActions",
	Schedule:   "@every 10s",
	JobHistory: true,
	RunNow:     true,
	Singleton:  false,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypePlaybook
		ctx.History.ResourceID = api.UpstreamConf.Host
		if pulled, err := playbook.PullPlaybookAction(ctx.Context, api.UpstreamConf); err != nil {
			return err
		} else if pulled {
			ctx.History.SuccessCount = 1
		}

		return nil
	},
}

// PushPlaybookActions pushes actions that have been fully run to the upstream
var PushPlaybookActions = &job.Job{
	Name:       "PushPlaybookActions",
	Schedule:   "@every 10s",
	JobHistory: true,
	RunNow:     true,
	Singleton:  false,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypePlaybook
		ctx.History.ResourceID = api.UpstreamConf.Host
		if count, err := playbook.PushPlaybookActions(ctx.Context, api.UpstreamConf, 200); err != nil {
			return err
		} else {
			ctx.History.SuccessCount += count
		}

		return nil
	},
}

// PushArtifacts pushes artifacts to the upstream
var PushArtifacts = &job.Job{
	Name:       "PushArtifacts",
	Schedule:   "@every 10s",
	JobHistory: true,
	RunNow:     true,
	Singleton:  true,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypePlaybook
		ctx.History.ResourceID = api.UpstreamConf.Host
		count, err := upstream.SyncArtifacts(ctx.Context, api.UpstreamConf, 200)
		if err != nil {
			return err
		}
		ctx.History.SuccessCount += count
		return nil
	},
}
