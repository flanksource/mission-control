package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/responder"
	"github.com/robfig/cron/v3"
)

const (
	TeamComponentOwnershipSchedule  = "@every 15m"
	EvaluateEvidenceScriptsSchedule = "@every 5m"
	ResponderCommentsSyncSchedule   = "@every 1h"
	ResponderConfigSyncSchedule     = "@every 1h"
)

var FuncScheduler = cron.New()

func ScheduleFunc(schedule string, fn func()) (any, error) {
	return FuncScheduler.AddFunc(schedule, fn)
}

func Start() {
	// Running first at startup and then with the schedule
	TeamComponentOwnershipRun()
	EvaluateEvidenceScripts()
	responder.SyncComments()
	responder.SyncConfig()

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

	FuncScheduler.Start()
}
