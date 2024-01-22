package playbook

import (
	"fmt"
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/events"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	// register event handlers
	_ "github.com/flanksource/incident-commander/incidents/responder"
	_ "github.com/flanksource/incident-commander/notification"
)

func TestPlaybook(t *testing.T) {
	tempPath = fmt.Sprintf("%s/config-class.txt", t.TempDir())
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Playbook")
}

var (
	// tempPath is used to store the result of the action for this test.
	tempPath string

	echoServerPort int

	shutdownEcho func()

	DefaultContext context.Context
)

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	_ = context.UpdateProperty(DefaultContext, api.PropertyIncidentsDisabled, "true")

	events.StartConsumers(DefaultContext)
	StartPlaybookConsumers(DefaultContext)

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.SetRequest(c.Request().WithContext(DefaultContext.Wrap(c.Request().Context())))
			return next(c)
		}
	})
	e.Use(auth.MockMiddleware)
	RegisterRoutes(e)
	echoServerPort, shutdownEcho = setup.RunEcho(e)
})

var _ = ginkgo.AfterSuite(func() {
	shutdownEcho()
	// setup.DumpEventQueue(DefaultContext)
	setup.AfterSuiteFn()
})
