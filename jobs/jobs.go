package jobs

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/upstream"
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

func Start(ctx api.Context) {
	if err := newFuncJob(TeamComponentOwnershipRun, TeamComponentOwnershipSchedule).
		runOnStart().addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule sync jobs for team component: %v", err)
	}

	if err := newFuncJob(EvaluateEvidenceScripts, EvaluateEvidenceScriptsSchedule).
		runOnStart().addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for evidence script evaluation: %v", err)
	}

	if err := newFuncJob(responder.SyncComments, ResponderCommentsSyncSchedule).
		runOnStart().addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for syncing responder comments: %v", err)
	}

	if err := newFuncJob(responder.SyncConfig, ResponderConfigSyncSchedule).
		runOnStart().addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for syncing responder config: %v", err)
	}

	if err := newFuncJob(CleanupJobHistoryTable, CleanupJobHistoryTableSchedule).
		runOnStart().addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up job history table: %v", err)
	}

	if err := newFuncJob(CleanupEventQueue, CleanupEventQueueTableSchedule).
		addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up event queue table: %v", err)
	}

	if err := newFuncJob(CleanupNotificationSendHistory, CleanupNotificationSendHistorySchedule).
		addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up notification send history table: %v", err)
	}

	if api.UpstreamConf.Valid() {
		if err := newFuncJob(SyncWithUpstream, PushAgentReconcileSchedule).
			setName("UpstreamReconcile").runOnStart().setTimeout(time.Minute * 10).
			addToScheduler(FuncScheduler); err != nil {
			logger.Errorf("Failed to schedule push reconcile job: %v", err)
		}

		// TODO: This check status job needs to be moved to use a job runner struct like newFuncJob which works with struct
		checkstatusJob := &checkstatusSyncJob{upstreamClient: upstream.NewUpstreamClient(api.UpstreamConf)}
		checkstatusJob.Run()

		if _, err := FuncScheduler.AddJob(PushCheckStatusesSchedule, checkstatusJob); err != nil {
			logger.Errorf("Failed to schedule check status sync job: %v", err)
		}
	}

	incidentRulesSchedule := fmt.Sprintf("@every %s", rules.Period.String())
	if err := newFuncJob(rules.Run, incidentRulesSchedule).
		setName("IncidentRules").runOnStart().addToScheduler(FuncScheduler); err != nil {
		logger.Errorf("Failed to schedule job for incident rules: %v", err)
	}

	FuncScheduler.Start()
}
