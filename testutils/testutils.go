package testutils

import (
	"context"
	"fmt"
	"io"
	"os"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/jackc/pgx/v5/pgxpool"
	ginkgo "github.com/onsi/ginkgo/v2"
	"gorm.io/gorm"
)

func GetPGConfig(database string, port int) embeddedPG.Config {
	// We are firing up multiple instances of the embedded postgres server at once when running tests in parallel.
	//
	// By default fergusstrange/embedded-postgres directly extracts the Postgres binary to a set location
	// (/home/runner/.embedded-postgres-go/extracted/bin/initdb) and starts it.
	// If two instances try to do this at the same time, they conflict, and throw the error
	// "unable to extract postgres archive: open /home/runner/.embedded-postgres-go/extracted/bin/initdb: text file busy."
	//
	// This is a way to have separate instances of the running postgres servers.

	var runTimePath string
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Errorf("error getting user home dir: %v", err)
		runTimePath = fmt.Sprintf("/tmp/.embedded-postgres-go/extracted-%d", port)
	} else {
		runTimePath = fmt.Sprintf("%s/.embedded-postgres-go/extracted-%d", homeDir, port)
	}

	return embeddedPG.DefaultConfig().
		Database(database).
		Port(uint32(port)).
		RuntimePath(runTimePath).
		Logger(io.Discard)
}

func SetupDBConnection(dbName string, port int) (*gorm.DB, *pgxpool.Pool) {
	connectionString := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/%s?sslmode=disable", port, dbName)

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
