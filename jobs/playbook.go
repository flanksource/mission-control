package jobs

import (
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/api"

	"github.com/flanksource/incident-commander/playbook"
)

// PullPlaybookActions periodically pulls playbook actions to run
// from the upstream
var PullPlaybookActions = &job.Job{
	Name:       "PullPlaybookActions",
	Schedule:   "@every 60s",
	Retention:  job.RetentionFailed,
	JobHistory: true,
	RunNow:     true,
	Singleton:  false,
	Fn: func(ctx job.JobRuntime) error {
		ctx.History.ResourceType = job.ResourceTypePlaybook
		ctx.History.ResourceID = api.UpstreamConf.Host

		return playbook.PullPlaybookAction(ctx, api.UpstreamConf)
	},
}

// PullPlaybookActions pushes actions, that have been fully run, to the upstream
var PushPlaybookActions = &job.Job{
	Name:       "PushPlaybookActions",
	Schedule:   "@every 60s",
	Retention:  job.RetentionFailed,
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
