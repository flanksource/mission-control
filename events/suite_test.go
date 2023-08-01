package events

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/testutils"
	"github.com/flanksource/incident-commander/api"

	"github.com/flanksource/incident-commander/upstream"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func TestPushMode(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Push Mode Suite")
}

var (
	// postgres server shared by both agent and upstream
	postgresServer *embeddedPG.EmbeddedPostgres
	pgServerPort   = 9879

	upstreamEchoServerport = 11005
	upstreamEchoServer     *echo.Echo

	agentDB       *gorm.DB
	agentDBPGPool *pgxpool.Pool
	agentDBName   = "agent"

	upstreamDB       *gorm.DB
	upstreamDBPGPool *pgxpool.Pool
	upstreamDBName   = "upstream"
)

var _ = ginkgo.BeforeSuite(func() {
	config, connection := testutils.GetEmbeddedPGConfig(agentDBName, pgServerPort)
	postgresServer = embeddedPG.NewDatabase(config)
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", pgServerPort)

	var err error
	if agentDB, agentDBPGPool, err = duty.SetupDB(connection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}
	if err := dummy.PopulateDBWithDummyModels(agentDB); err != nil {
		ginkgo.Fail(err.Error())
	}

	_, err = agentDBPGPool.Exec(context.TODO(), fmt.Sprintf("CREATE DATABASE %s", upstreamDBName))
	Expect(err).NotTo(HaveOccurred())

	upstreamDBConnection := strings.ReplaceAll(connection, agentDBName, upstreamDBName)
	if upstreamDB, upstreamDBPGPool, err = duty.SetupDB(upstreamDBConnection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	upstreamEchoServer = echo.New()
	upstreamEchoServer.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := api.NewContext(upstreamDB, c)
			return next(cc)
		}
	})
	upstreamEchoServer.POST("/upstream/push", upstream.PushUpstream)
	listenAddr := fmt.Sprintf(":%d", upstreamEchoServerport)

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
