package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/incidents"
	"github.com/robfig/cron/v3"
)

const (
	TeamComponentOwnershipSchedule         = "@every 15m"
	CleanupJobHistoryTableSchedule         = "@every 24h"
	CleanupEventQueueTableSchedule         = "@every 24h"
	CleanupNotificationSendHistorySchedule = "@every 24h"
)

var FuncScheduler = cron.New()

func Start(ctx context.Context) {
	if err := job.NewJob(ctx, "Team Component Ownership", "@every 15m", TeamComponentOwnershipRun).
		RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule sync jobs for team component: %v", err)
	}

	if err := job.NewJob(ctx, "Cleanup JobHistory Table", CleanupJobHistoryTableSchedule, CleanupJobHistoryTable).
		RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up job history table: %v", err)
	}

	if err := job.NewJob(ctx, "Cleanup Event Queue", CleanupEventQueueTableSchedule, CleanupEventQueue).
		AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up event queue table: %v", err)
	}

	if err := job.NewJob(ctx, "Cleanup NotificationSend History", CleanupNotificationSendHistorySchedule, CleanupNotificationSendHistory).
		AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up notification send history table: %v", err)
	}

	if api.UpstreamConf.Valid() {
		for _, job := range []*job.Job{SyncCheckStatuses, SyncWithUpstream} {
			j := job
			if err := j.AddToScheduler(FuncScheduler); err != nil {
				logger.Errorf("Failed to schedule %s: %v", j, err)
			}
		}
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
