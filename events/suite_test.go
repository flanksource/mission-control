package events

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/incident-commander/testutils"
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
	postgresServer *embeddedPG.EmbeddedPostgres
	testEchoServer *echo.Echo
)

func setup(dbName, connectionString string) (*gorm.DB, *pgxpool.Pool) {
	pgxpool, err := duty.NewPgxPool(connectionString)
	if err != nil {
		ginkgo.Fail(err.Error())
	}
	conn, err := pgxpool.Acquire(context.Background())
	if err != nil {
		ginkgo.Fail(err.Error())
	}
	defer conn.Release()

	gormDB, err := duty.NewGorm(connectionString, duty.DefaultGormConfig())
	if err != nil {
		ginkgo.Fail(err.Error())
	}

	if err = duty.Migrate(connectionString, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	return gormDB, pgxpool
}

var _ = ginkgo.BeforeSuite(func() {
	config := testutils.GetPGConfig("test", testutils.TestPostgresPort)
	postgresServer = embeddedPG.NewDatabase(config)
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", testutils.TestPostgresPort)

	gormDB, pgxpool := setup("test", testutils.PGUrl)
	testutils.TestDB = gormDB
	testutils.TestDBPGPool = pgxpool

	if err := dummy.PopulateDBWithDummyModels(testutils.TestDB); err != nil {
		ginkgo.Fail(err.Error())
	}

	_, err := testutils.TestDBPGPool.Exec(context.TODO(), "CREATE DATABASE upstream")
	Expect(err).NotTo(HaveOccurred())

	udb, upgxpool := setup("upstream", testutils.UpstreamPGUrl)
	testutils.TestUpstreamDB = udb
	testutils.TestUpstreamDBPGPool = upgxpool

	testEchoServer = echo.New()
	testEchoServer.POST("/upstream_push", upstream.PushUpstream)
	listenAddr := fmt.Sprintf(":%d", testutils.TestUpstreamServerPort)

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.
		if err := testEchoServer.Start(listenAddr); err != nil {
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
	if err := testEchoServer.Shutdown(context.Background()); err != nil {
		ginkgo.Fail(err.Error())
	}

	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})
