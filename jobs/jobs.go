package jobs

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron/v3"
)

const (
	TeamComponentOwnershipSchedule  = "@every 15m"
	EvaluateEvidenceScriptsSchedule = "@every 5m"
)

// Cache for storing compiled CEL scripts
var prgCache *cache.Cache

var FuncScheduler = cron.New()

func ScheduleFunc(schedule string, fn func()) (any, error) {
	return FuncScheduler.AddFunc(schedule, fn)
}

func Start() {
	prgCache = cache.New(24*time.Hour, 1*time.Hour)

	// Running first at startup and then with the schedule
	TeamComponentOwnershipRun()
	EvaluateEvidenceScripts()

	if _, err := ScheduleFunc(TeamComponentOwnershipSchedule, TeamComponentOwnershipRun); err != nil {
		logger.Errorf("Failed to schedule sync jobs for team component: %v", err)
	}

	if _, err := ScheduleFunc(EvaluateEvidenceScriptsSchedule, EvaluateEvidenceScripts); err != nil {
		logger.Errorf("Failed to schedule job for evidence script evaluation: %v", err)
	}

	FuncScheduler.Start()
}
