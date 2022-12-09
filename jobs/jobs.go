package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/robfig/cron/v3"
)

const (
	TeamComponentOwnershipSchedule  = "@every 15m"
	EvaluateEvidenceScriptsSchedule = "@every 30m"
)

var FuncScheduler = cron.New()

func ScheduleFunc(schedule string, fn func()) (any, error) {
	return FuncScheduler.AddFunc(schedule, fn)
}

func Start() {
	if _, err := ScheduleFunc(TeamComponentOwnershipSchedule, TeamComponentOwnershipRun); err != nil {
		logger.Errorf("Failed to schedule sync jobs for team component: %v", err)
	}

	if _, err := ScheduleFunc(EvaluateEvidenceScriptsSchedule, EvaluateEvidenceScripts); err != nil {
		logger.Errorf("Failed to schedule job for evidence script evaluation: %v", err)
	}

	// Running first at startup and then with the schedule
	TeamComponentOwnershipRun()
	EvaluateEvidenceScripts()

	FuncScheduler.Start()
}
