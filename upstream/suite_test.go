package upstream

import (
	"context"
	"io"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/jackc/pgx/v5/pgxpool"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

func TestPushMode(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Push Mode Suite")
}

const pgUrl = "postgres://postgres:postgres@localhost:9876/test?sslmode=disable"

var (
	postgresServer *embeddedPG.EmbeddedPostgres
	testDB         *gorm.DB
	testDBPGPool   *pgxpool.Pool
	testClient     kubernetes.Interface
)

var _ = ginkgo.BeforeSuite(func() {
	postgresServer = embeddedPG.NewDatabase(embeddedPG.DefaultConfig().
		Database("test").
		Port(9876).
		Logger(io.Discard))
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port 9876")

	var err error
	testDBPGPool, err = duty.NewPgxPool(pgUrl)
	if err != nil {
		ginkgo.Fail(err.Error())
	}
	conn, err := testDBPGPool.Acquire(context.Background())
	if err != nil {
		ginkgo.Fail(err.Error())
	}
	defer conn.Release()

	testDB, err = duty.NewGorm(pgUrl, duty.DefaultGormConfig())
	if err != nil {
		ginkgo.Fail(err.Error())
	}

	if err = duty.Migrate(pgUrl); err != nil {
		ginkgo.Fail(err.Error())
	}

	if err := populateMonitoredTables(testDB); err != nil {
		ginkgo.Fail(err.Error())
	}
})

var _ = ginkgo.AfterSuite(func() {
	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})
