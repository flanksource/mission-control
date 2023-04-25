package upstream

import (
	"context"
	"io"
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
	upstreamPGUrl          = "postgres://postgres:postgres@localhost:9877/upstream?sslmode=disable"
	testUpstreamServerPort = 11005
)

var (
	postgresServer *embeddedPG.EmbeddedPostgres
	testDB         *gorm.DB
	testDBPGPool   *pgxpool.Pool

	upstreamPostgresServer *embeddedPG.EmbeddedPostgres
	testUpstreamDB         *gorm.DB
	testUpstreamDBPGPool   *pgxpool.Pool

	testEchoServer *echo.Echo
)

func setup(pgPort uint32, dbName, connectionString string) (*gorm.DB, *pgxpool.Pool, *embeddedPG.EmbeddedPostgres) {
	pgServer := embeddedPG.NewDatabase(embeddedPG.DefaultConfig().Database(dbName).Port(pgPort).DataPath("/tmp/embedded-pg/" + dbName).Logger(io.Discard))
	if err := pgServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", pgPort)

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

	return gormDB, pgxpool, pgServer
}

var _ = ginkgo.BeforeSuite(func() {
	db, pgxpool, pgServer := setup(9876, "test", pgUrl)
	testDB = db
	testDBPGPool = pgxpool
	postgresServer = pgServer

	if err := populateMonitoredTables(testDB); err != nil {
		ginkgo.Fail(err.Error())
	}

	udb, upgxpool, pgServer := setup(9877, "upstream", upstreamPGUrl)
	testUpstreamDB = udb
	testUpstreamDBPGPool = upgxpool
	upstreamPostgresServer = pgServer
})

var _ = ginkgo.AfterSuite(func() {
	if testEchoServer != nil {
		testEchoServer.Shutdown(context.Background())
	}

	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})
