package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
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

const (
	pgUrl                  = "postgres://postgres:postgres@localhost:9876/test?sslmode=disable"
	upstreamPGUrl          = "postgres://postgres:postgres@localhost:9876/upstream?sslmode=disable"
	testUpstreamServerPort = 11005
)

var (
	postgresServer *embeddedPG.EmbeddedPostgres
	testDB         *gorm.DB
	testDBPGPool   *pgxpool.Pool

	testUpstreamDB       *gorm.DB
	testUpstreamDBPGPool *pgxpool.Pool

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

	if err = duty.Migrate(connectionString); err != nil {
		ginkgo.Fail(err.Error())
	}

	return gormDB, pgxpool
}

var _ = ginkgo.BeforeSuite(func() {
	postgresServer = embeddedPG.NewDatabase(embeddedPG.DefaultConfig().Database("test").Port(9876).Logger(io.Discard))
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", 9876)

	gormDB, pgxpool := setup("test", pgUrl)
	testDB = gormDB
	testDBPGPool = pgxpool

	if err := populateMonitoredTables(testDB); err != nil {
		ginkgo.Fail(err.Error())
	}

	_, err := testDBPGPool.Exec(context.TODO(), "CREATE DATABASE upstream")
	Expect(err).NotTo(HaveOccurred())

	udb, upgxpool := setup("upstream", upstreamPGUrl)
	testUpstreamDB = udb
	testUpstreamDBPGPool = upgxpool

	testEchoServer = echo.New()
	testEchoServer.POST("/upstream_push", PushUpstream)
	listenAddr := fmt.Sprintf(":%d", testUpstreamServerPort)

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
