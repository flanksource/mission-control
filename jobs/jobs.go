package jobs

import (
	gocontext "context"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/incidents"
	"github.com/flanksource/incident-commander/notification"
	"github.com/robfig/cron/v3"
	"github.com/sethvargo/go-retry"
)

const (
	TeamComponentOwnershipSchedule         = "@every 15m"
	CleanupJobHistoryTableSchedule         = "@every 24h"
	CleanupEventQueueTableSchedule         = "@every 24h"
	CleanupNotificationSendHistorySchedule = "@every 24h"
)

var FuncScheduler = cron.New()

var agentJobs = []*job.Job{
	PingUpstream,
	ReconcileAll,
	SyncArtifactData,
	PushPlaybookActions,
}

func RunPullPlaybookActionsJob(ctx context.Context) {
	// use a single job instance to maintain retention
	job := PullPlaybookActions(ctx)

	for {
		backoff := retry.WithMaxRetries(10, retry.NewExponential(time.Second))
		_ = retry.Do(ctx, backoff, func(_ctx gocontext.Context) error {
			job.Run()

			if len(job.LastJob.Errors) != 0 {
				return retry.RetryableError(fmt.Errorf("%s", strings.Join(job.LastJob.Errors, ", ")))
			}

			return nil
		})
	}
}

func Start(ctx context.Context) {
	if err := job.NewJob(ctx, "Team Component Ownership", "@every 15m", TeamComponentOwnershipRun).
		RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule sync jobs for team component: %v", err)
	}

	if err := job.NewJob(ctx, "Cleanup Event Queue", CleanupEventQueueTableSchedule, CleanupEventQueue).
		AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up event queue table: %v", err)
	}

	if err := notification.ProcessPendingNotificationsJob(ctx).AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("failed to schedule job: %v", err)
	}

	if err := job.NewJob(ctx, "Cleanup NotificationSend History", CleanupNotificationSendHistorySchedule, CleanupNotificationSendHistory).
		AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up notification send history table: %v", err)
	}

	query.SyncConfigCacheJob.Context = ctx
	if err := query.SyncConfigCacheJob.RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for syncing config cache: %v", err)
	}

	for _, job := range CatalogRefreshJobs {
		j := job
		j.Context = ctx
		if err := j.AddToScheduler(FuncScheduler); err != nil {
			logger.Errorf("Failed to schedule %s: %v", j, err)
		}
	}

	if api.UpstreamConf.Valid() {
		for _, job := range agentJobs {
			j := job
			j.Context = ctx
			if err := j.AddToScheduler(FuncScheduler); err != nil {
				logger.Errorf("Failed to schedule %s: %v", j, err)
			}
		}

		go RunPullPlaybookActionsJob(ctx)
	}

	cleanupStaleJobHistory.Context = ctx
	if err := cleanupStaleJobHistory.AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up stale job history: %v", err)
	}

	cleanupStaleAgentJobHistory.Context = ctx
	if err := cleanupStaleAgentJobHistory.AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up stale agent job history: %v", err)
	}

	startIncidentsJobs(ctx)

	FuncScheduler.Start()
}

func startIncidentsJobs(ctx context.Context) {
	if disabled := ctx.Properties()[api.PropertyIncidentsDisabled]; disabled == "true" {
		logger.Debugf("Skipping incidents jobs")
		return
	}

	for _, job := range incidents.IncidentJobs {
		j := job
		j.Context = ctx
		if err := j.AddToScheduler(FuncScheduler); err != nil {
			logger.Errorf("Failed to schedule job %s: %v", j, err)
		}
	}

}
