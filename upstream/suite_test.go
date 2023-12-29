package upstream

import (
	gocontext "context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func TestUpstream(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Upstream")
}

var (
	// postgres server shared by both agent and upstream
	postgresServer *embeddedPG.EmbeddedPostgres
	pgServerPort   = 9884

	upstreamEchoServerport = 11006
	upstreamEchoServer     *echo.Echo

	agentID       = uuid.New()
	agentName     = "test-agent"
	agentDB       *gorm.DB
	agentDBPGPool *pgxpool.Pool
	agentDBName   = "agent"

	upstreamDBName = "upstream"
	upstreamDB     *gorm.DB
	upstreamPool   *pgxpool.Pool
	dummyDataset   dummy.DummyData
)

var _ = ginkgo.BeforeSuite(func() {
	var err error
	config, connection := setup.GetEmbeddedPGConfig(agentDBName, pgServerPort)
	postgresServer = embeddedPG.NewDatabase(config)
	if err = postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", pgServerPort)

	if agentDB, agentDBPGPool, err = duty.SetupDB(connection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}
	dummyDataset = dummy.GetStaticDummyData(agentDB)

	_, err = agentDBPGPool.Exec(gocontext.TODO(), fmt.Sprintf("CREATE DATABASE %s", upstreamDBName))
	Expect(err).NotTo(HaveOccurred())

	upstreamDBConnection := strings.ReplaceAll(connection, agentDBName, upstreamDBName)
	if upstreamDB, upstreamPool, err = duty.SetupDB(upstreamDBConnection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}
	Expect(upstreamDB.Create(&models.Agent{ID: agentID, Name: agentName}).Error).To(BeNil())

	setupUpstreamHTTPServer()
})

var _ = ginkgo.AfterSuite(func() {
	logger.Infof("Stopping upstream echo server")
	if err := upstreamEchoServer.Shutdown(gocontext.Background()); err != nil {
		ginkgo.Fail(err.Error())
	}

	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})

func setupUpstreamHTTPServer() {
	upstreamEchoServer = echo.New()
	upstreamEchoServer.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.SetRequest(c.Request().WithContext(context.NewContext(c.Request().Context()).WithDB(upstreamDB, upstreamPool)))
			return next(c)
		}
	})

	api.DefaultContext = context.NewContext(gocontext.Background()).WithDB(upstreamDB, upstreamPool)
	upstreamGroup := upstreamEchoServer.Group("/upstream")
	upstreamGroup.POST("/push", PushUpstream)
	upstreamGroup.GET("/pull/:agent_name", Pull)
	upstreamGroup.GET("/status/:agent_name", Status)
	listenAddr := fmt.Sprintf(":%d", upstreamEchoServerport)

	api.UpstreamConf = upstream.UpstreamConfig{
		AgentName: agentName,
		Host:      fmt.Sprintf("http://localhost:%d", upstreamEchoServerport),
		Username:  "admin@local",
		Password:  "admin",
		Labels:    []string{"test"},
	}

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.
		if err := upstreamEchoServer.Start(listenAddr); err != nil {
			if err == http.ErrServerClosed {
				logger.Infof("Server closed")
			} else {
				ginkgo.Fail(fmt.Sprintf("Failed to start test server: %v", err))
			}
		}
	}()
}
