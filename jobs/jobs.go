package jobs

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/rules"
	"github.com/robfig/cron/v3"
)

const (
	TeamComponentOwnershipSchedule         = "@every 15m"
	EvaluateEvidenceScriptsSchedule        = "@every 5m"
	ResponderCommentsSyncSchedule          = "@every 1h"
	ResponderConfigSyncSchedule            = "@every 1h"
	CleanupJobHistoryTableSchedule         = "@every 24h"
	CleanupEventQueueTableSchedule         = "@every 24h"
	CleanupNotificationSendHistorySchedule = "@every 24h"
	PushAgentReconcileSchedule             = "@every 8h"
	PushCheckStatusesSchedule              = "@every 30s"
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
		if err := job.NewJob(ctx, "Upstream Reconcile", PushAgentReconcileSchedule, SyncWithUpstream).
			RunOnStart().SetTimeout(time.Minute * 10).
			AddToScheduler(FuncScheduler); err != nil {
			logger.Errorf("Failed to schedule push reconcile job: %v", err)
		}

		if err := job.NewJob(ctx, "SyncCheckStatuses", PushCheckStatusesSchedule, SyncCheckStatuses).
			RunOnStart().
			AddToScheduler(FuncScheduler); err != nil {
			logger.Errorf("Failed to schedule check statusese sync job: %v", err)
		}
	}

	startIncidentsJobs(ctx)

	FuncScheduler.Start()
}

func startIncidentsJobs(ctx context.Context) {
	var incidentDisabled bool
	if err := ctx.DB().Raw("SELECT true FROM properties WHERE name = ? AND value = 'true' AND deleted_at IS NULL", api.PropertyIncidentsDisabled).Scan(&incidentDisabled).Error; err != nil {
		logger.Errorf("Failed to fetch incidents disabled flag: %v", err)
		return
	}

	if incidentDisabled {
		logger.Debugf("Skipping incidents jobs")
		return
	}

	if err := job.NewJob(ctx, "Evaluate Evidence Scripts", EvaluateEvidenceScriptsSchedule, EvaluateEvidenceScripts).
		RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for evidence script evaluation: %v", err)
	}

	if err := job.NewJob(ctx, "Sync Responder Comments", ResponderCommentsSyncSchedule, responder.SyncComments).
		RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for syncing responder comments: %v", err)
	}

	if err := job.NewJob(ctx, "Sync Responder Config", ResponderConfigSyncSchedule, responder.SyncConfig).
		RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for syncing responder config: %v", err)
	}

	incidentRulesSchedule := fmt.Sprintf("@every %s", rules.Period.String())
	if err := job.NewJob(ctx, "Incident Rules", incidentRulesSchedule, rules.Run).
		RunOnStart().AddToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for incident rules: %v", err)
	}
}
