package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/robfig/cron/v3"
)

type funcJob struct {
	name    string                      // name is just an additional context for the job.
	timeout time.Duration               // optional timeout for the job
	fn      func(context.Context) error // the actual job
	runNow  bool                        // whether to run the job now
}

func (t funcJob) Run() {
	ctx := context.Background()
	if t.timeout > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}

	if err := t.fn(ctx); err != nil {
		logger.Errorf("%s: %v", t.name, err)
	}
}

func (t funcJob) schedule(cronRunner *cron.Cron, schedule string) error {
	_, err := cronRunner.AddJob(schedule, t)
	if err != nil {
		return fmt.Errorf("failed to schedule job: %s", t.name)
	}

	if t.runNow {
		logger.Infof("Running job now:: %s", t.name)
		t.Run()
	}

	return nil
}

func newFuncJob(fn func(context.Context) error, opts ...func(*funcJob)) *funcJob {
	job := &funcJob{
		fn: fn,
	}

	for i := range opts {
		opts[i](job)
	}

	return job
}

func withName(name string) func(*funcJob) {
	return func(j *funcJob) {
		j.name = name
	}
}

func withTimeout(timeout time.Duration) func(*funcJob) {
	return func(j *funcJob) {
		j.timeout = timeout
	}
}

func withRunNow(runNow bool) func(*funcJob) {
	return func(j *funcJob) {
		j.runNow = runNow
	}
}
