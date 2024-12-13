package jobs

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/api"

	"github.com/flanksource/incident-commander/playbook"
)

// PullPlaybookActions pulls playbook actions to run from the upstream
// NOTE: This job isn't run by the cron scheduler.
func PullPlaybookActions(ctx context.Context) *job.Job {
	return &job.Job{
		Name:       "PullPlaybookActions",
		Retention:  job.RetentionFailed,
		JobHistory: true,
		RunNow:     true,
		Context:    ctx,
		Singleton:  false,
		Fn: func(ctx job.JobRuntime) error {
			ctx.History.ResourceType = job.ResourceTypePlaybook
			ctx.History.ResourceID = api.UpstreamConf.Host

			return playbook.PullPlaybookAction(ctx, api.UpstreamConf)
		},
	}
}

// PullPlaybookActions pushes actions, that have been fully run, to the upstream
var PushPlaybookActions = &job.Job{
	Name:       "PushPlaybookActions",
	Schedule:   "@every 5s", // TODO: Find a way to push the newly updated actions with least amount of delay.
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
