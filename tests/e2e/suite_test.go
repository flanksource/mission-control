package e2e

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/samber/lo"
	"github.com/samber/oops"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/sdk"
	"github.com/flanksource/incident-commander/playbook/testdata"
	"github.com/flanksource/incident-commander/metrics"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/vars"

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
	tempPath       string
	e              *echo.Echo
	server         *httptest.Server
	metricsServer  *httptest.Server
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

	// TODO: add a system user to dummy fixtures
	api.SystemUserID = &dummy.JohnDoe.ID

	api.DefaultArtifactConnection = "connection://default/artifacts"

	if err := rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter); err != nil {
		ginkgo.Fail(err.Error())
	}

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
	metrics.RegisterDBStats(DefaultContext)
	e = echoSrv.New(DefaultContext)
	e.Use(auth.MockAuthMiddleware)

	events.StartConsumers(DefaultContext)
	if err := playbook.StartPlaybookConsumers(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}

	server = httptest.NewServer(e)
	metricsServer = httptest.NewServer(echoSrv.MetricsHandler())
	client = sdk.NewPlaybookClient(server.URL)
	client.Client = client.
		Auth(dummy.JohnDoe.Name, "admin").
		Header("X-Trace", "true")
	logger.Infof("Started test server @ %s", server.URL)
	logger.Infof("Started metrics server @ %s", metricsServer.URL)

	if err := testdata.LoadConnections(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}
	if err := testdata.LoadPermissions(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
	if server != nil {
		server.Close()
	}
	if metricsServer != nil {
		metricsServer.Close()
	}
})
