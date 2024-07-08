package push

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	"github.com/labstack/echo/v4"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	DefaultContext    context.Context
	PushServerContext context.Context

	echoServerPort int

	shutdownEcho func()

	shutdown func()
)

func TestPush(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Push")
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

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn(setup.WithoutDummyData)
	PushServerContext = setup.BeforeSuiteFn(setup.WithoutDummyData)

	if context, drop, err := setup.NewDB(PushServerContext, "push_server"); err != nil {
		ginkgo.Fail(err.Error())
	} else {
		PushServerContext = *context
		shutdown = wrap(shutdown, drop)
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.SetRequest(c.Request().WithContext(PushServerContext.Wrap(c.Request().Context())))
			return next(c)
		}
	})
	e.POST("/push/topology", PushTopology)

	echoServerPort, shutdownEcho = setup.RunEcho(e)
})

var _ = ginkgo.AfterSuite(func() {
	if shutdown != nil {
		shutdown()
	}

	shutdownEcho()

	setup.AfterSuiteFn()
})
