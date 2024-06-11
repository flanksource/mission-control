package playbook

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/events"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/patrickmn/go-cache"

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
	if err := StartPlaybookConsumers(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.SetRequest(c.Request().WithContext(DefaultContext.Wrap(c.Request().Context())))
			return next(c)
		}
	})
	e.Use(auth.MockMiddleware)
	RegisterRoutes(e)

	upstreamGroup := e.Group("/upstream", upstream.AgentAuthMiddleware(cache.New(24*time.Hour, 12*time.Hour)))
	upstreamGroup.POST("/push", upstream.PushHandler)
	upstreamGroup.GET("/playbook-action", func(c echo.Context) error {
		ctx := c.Request().Context().(context.Context)

		agent := ctx.Agent()
		if agent == nil {
			return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Err: "not found", Message: "agent not found"})
		}

		response, err := GetActionForAgent(ctx, agent)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}

		return c.JSON(http.StatusOK, response)
	})

	echoServerPort, shutdownEcho = setup.RunEcho(e)
})

var _ = ginkgo.AfterSuite(func() {
	shutdownEcho()
	// setup.DumpEventQueue(DefaultContext)
	setup.AfterSuiteFn()
})
