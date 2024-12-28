package push

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/duty/tests/setup"
	"github.com/labstack/echo/v4"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	DefaultContext    context.Context
	PushServerContext context.Context

	echoServerPort int
)

func TestPush(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Push")
}

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn(setup.WithoutDummyData)

	if context, drop, err := setup.NewDB(DefaultContext, "push_server"); err != nil {
		ginkgo.Fail(err.Error())
	} else {
		PushServerContext = *context
		shutdown.AddHookWithPriority("db drop", shutdown.PriorityCritical, drop)
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.SetRequest(c.Request().WithContext(PushServerContext.Wrap(c.Request().Context())))
			return next(c)
		}
	})
	e.POST("/push/topology", PushTopology)

	var shutdownEcho func()
	echoServerPort, shutdownEcho = setup.RunEcho(e)
	shutdown.AddHookWithPriority("shutdown Echo server", shutdown.PriorityCritical, shutdownEcho)
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
})
