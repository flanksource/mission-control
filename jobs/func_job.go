package jobs

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/robfig/cron/v3"
	"go.opentelemetry.io/otel"
)

type funcJob struct {
	name     string                  // name is just an additional context for the job.
	schedule string                  // job schedule
	timeout  time.Duration           // optional timeout for the job
	fn       func(api.Context) error // the actual job
	runNow   bool                    // whether to run the job now
}

func (t funcJob) Run() {
	ctx := api.DefaultContext
	tracer := otel.GetTracerProvider().Tracer("job-tracer")
	traceCtx, span := tracer.Start(ctx, "job-"+t.name)
	ctx = ctx.WithContext(traceCtx)
	defer span.End()

	if t.timeout > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, t.timeout)
		defer cancel()

		ctx = ctx.WithContext(timeoutCtx)
	}

	if err := t.fn(ctx); err != nil {
		logger.Errorf("%s: %v", t.name, err)
		span.RecordError(err)
	}
}

func getFunctionName(temp interface{}) string {
	fullName := runtime.FuncForPC(reflect.ValueOf(temp).Pointer()).Name()
	funcPath := strings.Split(fullName, "/")
	fnName := funcPath[len(funcPath)-1]
	return fnName
}

func (t funcJob) addToScheduler(cronRunner *cron.Cron) error {
	_, err := cronRunner.AddJob(t.schedule, t)
	if t.name == "" {
		t.name = getFunctionName(t.fn)
	}
	if err != nil {
		return fmt.Errorf("failed to schedule job: %s", t.name)
	}

	if t.runNow {
		logger.Infof("Running job now: %s", t.name)
		t.Run()
	}

	return nil
}

func newFuncJob(fn func(api.Context) error, schedule string) *funcJob {
	return &funcJob{
		fn:       fn,
		schedule: schedule,
	}
}

func (f *funcJob) setTimeout(timeout time.Duration) *funcJob {
	f.timeout = timeout
	return f
}

func (f *funcJob) setName(n string) *funcJob {
	f.name = n
	return f
}

func (f *funcJob) runOnStart() *funcJob {
	f.runNow = true
	return f
}
