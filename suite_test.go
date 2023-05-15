package main

import (
	"context"
	"io"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/testutils"
	"github.com/jackc/pgx/v5/pgxpool"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func TestMissionControl(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Mission Control Suite")
}

var (
	postgresServer *embeddedPG.EmbeddedPostgres
)

func setup(connectionString string) (*gorm.DB, *pgxpool.Pool) {
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
	postgresServer = embeddedPG.NewDatabase(embeddedPG.DefaultConfig().Database("test").Port(testutils.TestPostgresPort).Logger(io.Discard))
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", testutils.TestPostgresPort)

	db.Gorm, db.Pool = setup(testutils.PGUrl)
})

var _ = ginkgo.AfterSuite(func() {
	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})
