package jobs

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder"
	"github.com/flanksource/incident-commander/rules"
	"github.com/flanksource/incident-commander/upstream"
	"github.com/robfig/cron/v3"
)

type ErrHandlingFuncJob struct {
	name string // name is just an additional context for this job used when logging.
	fn   func() error
}

func (t ErrHandlingFuncJob) Run() {
	if err := t.fn(); err != nil {
		logger.Errorf("%s: %v", t.name, err)
	}
}

const (
	TeamComponentOwnershipSchedule  = "@every 15m"
	EvaluateEvidenceScriptsSchedule = "@every 5m"
	ResponderCommentsSyncSchedule   = "@every 1h"
	ResponderConfigSyncSchedule     = "@every 1h"
	CleanupJobHistoryTableSchedule  = "@every 24h"
	PushAgentReconcileSchedule      = "@every 30m"
)

var FuncScheduler = cron.New()

func ScheduleFunc(schedule string, fn func()) (any, error) {
	return FuncScheduler.AddFunc(schedule, fn)
}

func ScheduleErrHandlingFunc(schedule string, name string, fn func() error) (any, error) {
	return FuncScheduler.AddJob(schedule, ErrHandlingFuncJob{name, fn})
}

func Start() {
	// Running first at startup and then with the schedule
	TeamComponentOwnershipRun()
	EvaluateEvidenceScripts()
	responder.SyncComments()
	responder.SyncConfig()
	CleanupJobHistoryTable()
	if err := rules.Run(); err != nil {
		logger.Errorf("error running incident rules: %w", err)
	}

	if _, err := ScheduleFunc(TeamComponentOwnershipSchedule, TeamComponentOwnershipRun); err != nil {
		logger.Errorf("Failed to schedule sync jobs for team component: %v", err)
	}

	if _, err := ScheduleFunc(EvaluateEvidenceScriptsSchedule, EvaluateEvidenceScripts); err != nil {
		logger.Errorf("Failed to schedule job for evidence script evaluation: %v", err)
	}

	if _, err := ScheduleFunc(ResponderCommentsSyncSchedule, responder.SyncComments); err != nil {
		logger.Errorf("Failed to schedule job for syncing responder comments: %v", err)
	}

	if _, err := ScheduleFunc(ResponderConfigSyncSchedule, responder.SyncConfig); err != nil {
		logger.Errorf("Failed to schedule job for syncing responder config: %v", err)
	}

	if _, err := ScheduleFunc(CleanupJobHistoryTableSchedule, CleanupJobHistoryTable); err != nil {
		logger.Errorf("Failed to schedule job for cleaning up job history table: %v", err)
	}

	if api.UpstreamConf.Valid() {
		if _, err := ScheduleErrHandlingFunc(PushAgentReconcileSchedule, "upstream reoncile job", upstream.SyncWithUpstream); err != nil {
			logger.Errorf("Failed to schedule push reconcile job: %v", err)
		}
	}

	incidentRulesSchedule := fmt.Sprintf("@every %s", rules.Period.String())
	logger.Infof("IncidentRulesSchedule %s", incidentRulesSchedule)
	if _, err := ScheduleFunc(incidentRulesSchedule, func() {
		if err := rules.Run(); err != nil {
			logger.Errorf("error running incident rules: %w", err)
		}
	}); err != nil {
		logger.Errorf("Failed to schedule job for incident rules: %v", err)
	}

	FuncScheduler.Start()
}
