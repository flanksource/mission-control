package playbook

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook/sdk"
	"github.com/flanksource/incident-commander/vars"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/samber/lo"
	"github.com/samber/oops"

	// register event handlers
	_ "github.com/flanksource/incident-commander/incidents/responder"
	_ "github.com/flanksource/incident-commander/notification"
	_ "github.com/flanksource/incident-commander/rbac"
)

func TestPlaybook(t *testing.T) {
	tempPath = fmt.Sprintf("%s/config-class.txt", t.TempDir())
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Playbook")
}

var (
	// tempPath is used to store the result of the action for this test.
	tempPath       string
	e              *echo.Echo
	server         *httptest.Server
	DefaultContext context.Context
	client         sdk.PlaybookAPI
)

var _ = ginkgo.BeforeSuite(func() {
	format.RegisterCustomFormatter(func(value interface{}) (string, bool) {
		switch v := value.(type) {
		case error:
			if err, ok := oops.AsOops(v); ok {
				return fmt.Sprintf("%+v", err), true
			}
		}

		return "", false
	})
	DefaultContext = setup.BeforeSuiteFn()
	DefaultContext.Logger.SetLogLevel(DefaultContext.Properties().String("log.level", "info"))
	DefaultContext.Infof("%s", DefaultContext.String())

	format.RegisterCustomFormatter(func(value interface{}) (string, bool) {
		switch value.(type) {

		case *models.PlaybookRun, models.PlaybookRun:
			s := ""
			actions, _ := value.(models.PlaybookRun).GetActions(DefaultContext.DB())
			for _, action := range actions {
				s += fmt.Sprintf("\t\t%s: %s %s\n", action.Name, action.Status, lo.FromPtrOr(action.Error, ""))
			}
			return s, true

		}

		return "", false
	})

	vars.AuthMode = ""
	e = echoSrv.New(DefaultContext)
	e.Use(auth.MockAuthMiddleware)

	events.StartConsumers(DefaultContext)
	if err := StartPlaybookConsumers(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}

	server = httptest.NewServer(e)
	client = sdk.NewPlaybookClient(server.URL)
	client.Client = client.
		Auth(dummy.JohnDoe.Name, "admin").
		Header("X-Trace", "true")
	logger.Infof("Started test server @ %s", server.URL)
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
	server.Close()
})
