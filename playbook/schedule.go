package playbook

import (
	gocontext "context"
	"encoding/json"
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/vars"
)

type scheduleKey struct {
	PlaybookID uuid.UUID
	Index      int
}

type scheduleEntry struct {
	CronID   cron.EntryID
	Schedule string
}

var (
	scheduleMu      sync.Mutex
	scheduleEntries = make(map[scheduleKey]scheduleEntry)
)

func SchedulePlaybooks(ctx context.Context, scheduler *cron.Cron) *job.Job {
	return &job.Job{
		Name:       "SchedulePlaybooks",
		Schedule:   "@every 1m",
		Context:    ctx,
		Singleton:  true,
		Retention:  job.RetentionFailed,
		JobHistory: true,
		RunNow:     true,
		Fn: func(run job.JobRuntime) error {
			return syncPlaybookSchedules(run.Context, scheduler)
		},
	}
}

// SyncPlaybookSchedulesForTest is exported for use in e2e tests.
var SyncPlaybookSchedulesForTest = syncPlaybookSchedules

func syncPlaybookSchedules(ctx context.Context, scheduler *cron.Cron) error {
	var playbooks []models.Playbook
	if err := ctx.DB().
		Where("deleted_at IS NULL").
		Where("spec->'on'->'schedule' IS NOT NULL").
		Where("jsonb_array_length(spec->'on'->'schedule') > 0").
		Find(&playbooks).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to query scheduled playbooks")
	}

	scheduleMu.Lock()
	defer scheduleMu.Unlock()

	activeKeys := make(map[scheduleKey]bool)

	for _, pb := range playbooks {
		var spec v1.PlaybookSpec
		if err := json.Unmarshal(pb.Spec, &spec); err != nil {
			ctx.Errorf("failed to unmarshal playbook %s spec: %v", pb.ID, err)
			continue
		}

		if spec.On == nil {
			continue
		}

		for i, sched := range spec.On.Schedule {
			key := scheduleKey{PlaybookID: pb.ID, Index: i}
			activeKeys[key] = true

			existing, exists := scheduleEntries[key]
			if exists && existing.Schedule == sched.Schedule {
				continue
			}

			if exists {
				scheduler.Remove(existing.CronID)
				delete(scheduleEntries, key)
			}

			playbookID := pb.ID
			params := sched.Parameters
			cronID, err := scheduler.AddFunc(sched.Schedule, func() {
				// Create a fresh context for each cron execution rather than
				// capturing the sync job's context, which may be cancelled.
				cronCtx := context.NewContext(gocontext.Background()).
					WithDB(ctx.DB(), ctx.Pool()).
					WithNamespace(ctx.GetNamespace())
				triggerScheduledRun(cronCtx, playbookID, params)
			})
			if err != nil {
				ctx.Errorf("failed to register cron for playbook %s schedule[%d]: %v", pb.ID, i, err)
				continue
			}

			scheduleEntries[key] = scheduleEntry{
				CronID:   cronID,
				Schedule: sched.Schedule,
			}
		}
	}

	for key, entry := range scheduleEntries {
		if !activeKeys[key] {
			scheduler.Remove(entry.CronID)
			delete(scheduleEntries, key)
		}
	}

	return nil
}

func triggerScheduledRun(ctx context.Context, playbookID uuid.UUID, params map[string]string) {
	ctx = ctx.WithName("triggerScheduledRun").
		WithLoggingValues("playbook_id", playbookID.String(), "trigger", "schedule")

	var pb models.Playbook
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", playbookID).First(&pb).Error; err != nil {
		ctx.Errorf("failed to load playbook %s for scheduled run: %v", playbookID, err)
		return
	}

	run := models.PlaybookRun{
		PlaybookID: pb.ID,
		Status:     models.PlaybookRunStatusScheduled,
		Spec:       pb.Spec,
		Parameters: dutyTypes.JSONStringMap(params),
		Timeout:    ctx.Properties().Duration("playbook.run.timeout", vars.PlaybookRunTimeout),
	}

	if err := ctx.DB().Create(&run).Error; err != nil {
		ctx.Errorf("failed to create scheduled run for playbook %s: %v", playbookID, err)
	}
}
