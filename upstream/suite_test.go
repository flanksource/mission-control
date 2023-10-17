package upstream

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/testutils"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/contextwrapper"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
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
)

var _ = ginkgo.BeforeSuite(func() {
	var err error
	config, connection := testutils.GetEmbeddedPGConfig(agentDBName, pgServerPort)
	postgresServer = embeddedPG.NewDatabase(config)
	if err = postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", pgServerPort)

	if agentDB, agentDBPGPool, err = duty.SetupDB(connection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	_, err = agentDBPGPool.Exec(context.TODO(), fmt.Sprintf("CREATE DATABASE %s", upstreamDBName))
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
	if err := upstreamEchoServer.Shutdown(context.Background()); err != nil {
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
			cc := api.NewContext(upstreamDB, upstreamPool).WithEchoContext(c)
			return next(cc)
		}
	})

	api.ContextWrapFunc = contextwrapper.ContextWrapper(upstreamDB, upstreamPool, api.Kubernetes, otel.GetTracerProvider().Tracer("test"))
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
