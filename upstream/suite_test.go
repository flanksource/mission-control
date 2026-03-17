package upstream

import (
	"fmt"
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/upstream"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth"
)

var (
	DefaultContext context.Context

	shutdown func()
)

func TestUpstream(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Upstream")
}

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
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
	client      *upstream.UpstreamClient
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
		if err := t.dataset.Populate(t.WithDBLogLevel("info")); err != nil {
			ginkgo.Fail(err.Error())
		}
	}
}

func (t *agentWrapper) Reconcile(upstreamPort int) error {
	if t.client == nil {
		upstreamConfig := upstream.UpstreamConfig{
			AgentName: t.name,
			Host:      fmt.Sprintf("http://localhost:%d", upstreamPort),
			Username:  "System",
			Password:  "admin",
			Labels:    []string{"test"},
		}
		t.client = upstream.NewUpstreamClient(upstreamConfig)
	}

	summary := upstream.ReconcileAll(t.Context, t.client, 100)
	return summary.Error()
}

func (t *agentWrapper) StartServer() {
	e := echov4.New()

	e.Use(func(next echov4.HandlerFunc) echov4.HandlerFunc {
		return func(c echov4.Context) error {
			c.SetRequest(c.Request().WithContext(t.Context.Wrap(c.Request().Context())))
			return next(c)
		}
	})
	e.Use(auth.MockAuthMiddleware)
	RegisterRoutes(e)
	port, stop := setup.RunEcho(e)
	t.port = port

	shutdown = wrap(shutdown, stop)
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
