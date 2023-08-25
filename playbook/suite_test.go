package playbook

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/testutils"
	"github.com/flanksource/incident-commander/api"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func TestPlaybook(t *testing.T) {
	tempPath = fmt.Sprintf("%s/config-class.txt", t.TempDir())
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Playbook")
}

var (
	postgresServer *embeddedPG.EmbeddedPostgres
	pgServerPort   = 9885

	// tempPath is used to store the result of the action for this test.
	tempPath string

	echoServerPort = 11007
	echoServer     *echo.Echo

	testDB     *gorm.DB
	testDBPool *pgxpool.Pool
)

var _ = ginkgo.BeforeSuite(func() {
	config, connection := testutils.GetEmbeddedPGConfig("test", pgServerPort)
	postgresServer = embeddedPG.NewDatabase(config)
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", pgServerPort)

	var err error
	if testDB, testDBPool, err = duty.SetupDB(connection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	setupUpstreamHTTPServer()
})

var _ = ginkgo.AfterSuite(func() {
	logger.Infof("Stopping upstream echo server")
	if err := echoServer.Shutdown(context.Background()); err != nil {
		ginkgo.Fail(err.Error())
	}

	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})

func setupUpstreamHTTPServer() {
	echoServer = echo.New()
	echoServer.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := api.NewContext(testDB, c)
			return next(cc)
		}
	})

	echoServer.Use(mockAuthMiddleware)

	RegisterRoutes(echoServer, "playbook")

	listenAddr := fmt.Sprintf(":%d", echoServerPort)

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.
		if err := echoServer.Start(listenAddr); err != nil {
			if err == http.ErrServerClosed {
				logger.Infof("Server closed")
			} else {
				ginkgo.Fail(fmt.Sprintf("Failed to start test server: %v", err))
			}
		}
	}()
}

// mockAuthMiddleware doesn't actually authenticate since we never store auth data.
// It simply ensures that the requested user exists in the DB and then attaches the
// users's ID to the context.
func mockAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		name, _, ok := c.Request().BasicAuth()
		if !ok {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		var person models.Person
		if err := testDB.Where("name = ?", name).First(&person).Error; err != nil {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		ctx := c.(*api.Context)
		ctx.WithUser(&api.ContextUser{ID: person.ID, Email: person.Email})

		return next(c)
	}
}
