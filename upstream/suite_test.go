package upstream

import (
	"fmt"
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/postq"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var (
	DefaultContext context.Context

	shutdown func()
)

const batchSize = 5

func TestUpstream(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Upstream")
}

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	// DefaultContext = DefaultContext.WithDBLogLevel("trace").WithTrace()
})

var _ = ginkgo.AfterSuite(func() {
	if shutdown != nil {
		shutdown()
	}

	setup.AfterSuiteFn()
})

type agentWrapper struct {
	id   uuid.UUID // agent's id in the upstream db
	name string
	context.Context
	datasetFunc func(*gorm.DB) dummy.DummyData
	dataset     dummy.DummyData
	port        int
}

func (t *agentWrapper) setup(context context.Context) {
	if context, drop, err := setup.NewDB(context, t.name); err != nil {
		ginkgo.Fail(err.Error())
	} else {
		t.Context = *context
		shutdown = wrap(shutdown, drop)
	}

	if t.datasetFunc != nil {
		t.dataset = t.datasetFunc(t.DB())
		if err := t.dataset.Populate(t.WithDBLogLevel("info").DB()); err != nil {
			ginkgo.Fail(err.Error())
		}
	}

	// t.Context = t.WithDBLogLevel("trace").WithTrace()
}

func (t *agentWrapper) StartServer() {
	e := echov4.New()

	e.Use(func(next echov4.HandlerFunc) echov4.HandlerFunc {
		return func(c echov4.Context) error {
			c.SetRequest(c.Request().WithContext(t.Context.Wrap(c.Request().Context())))
			return next(c)
		}
	})
	e.Use(auth.MockMiddleware)
	RegisterRoutes(e)
	port, stop := setup.RunEcho(e)
	t.port = port

	shutdown = wrap(shutdown, stop)
}

func (t *agentWrapper) GetReconciler(other *agentWrapper) *upstream.UpstreamReconciler {
	upstreamConfig := upstream.UpstreamConfig{
		AgentName: t.name,
		Host:      fmt.Sprintf("http://localhost:%d", other.port),
		Username:  "System",
		Password:  "admin",
		Labels:    []string{"test"},
	}
	return upstream.NewUpstreamReconciler(upstreamConfig, batchSize)
}

func (t *agentWrapper) Reconcile(other *agentWrapper, table string) (int, error) {
	return t.GetReconciler(other).Sync(t.Context, table)
}

func (t *agentWrapper) PushTo(other agentWrapper) {
	upstreamConfig := upstream.UpstreamConfig{
		AgentName: t.name,
		Host:      fmt.Sprintf("http://localhost:%d", other.port),
		Username:  "System",
		Password:  "admin",
		Labels:    []string{"test"},
	}

	fn := upstream.NewPushUpstreamConsumer(upstreamConfig)
	consumer, err := postq.AsyncEventConsumer{
		WatchEvents: []string{api.EventPushQueueCreate},
		BatchSize:   50,
		Consumer: func(ctx postq.Context, e postq.Events) postq.Events {
			t.Context.Debugf("processing [%s] %d events", api.EventPushQueueCreate, len(e))
			e = fn(t.Context, e)
			t.Context.Debugf("processed [%s] %d events", api.EventPushQueueCreate, len(e))
			return e
		},
		ConsumerOption: &postq.ConsumerOption{
			ErrorHandler: func(e error) bool {
				defer ginkgo.GinkgoRecover()
				Expect(e).To(BeNil())
				return true
			},
		}}.EventConsumer()
	Expect(err).To(BeNil())
	consumer.ConsumeUntilEmpty(t.Context)

}

func wrap(fn1, fn2 func()) func() {
	if fn1 == nil {
		return fn2
	}
	return func() {
		fn1()
		fn2()
	}
}
