package jobs

import (
	"github.com/flanksource/commons/logger"
	"github.com/robfig/cron/v3"
)

const (
	TeamComponentOwnershipSchedule = "@every 15m"
)

var FuncScheduler = cron.New()

func ScheduleFunc(schedule string, fn func()) (interface{}, error) {
	return FuncScheduler.AddFunc(schedule, fn)
}

func Start() {
	// running first at startup and then with the schedule
	TeamComponentOwnershipRun()
	if _, err := ScheduleFunc(TeamComponentOwnershipSchedule, TeamComponentOwnershipRun); err != nil {
		logger.Errorf("Failed to schedule sync jobs for team component: %v", err)
	}

	FuncScheduler.Start()
}
