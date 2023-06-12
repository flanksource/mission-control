package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/migrate"
	_ "github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/pflag"
	"gorm.io/gorm"
)

var (
	ConnectionString string
	skipMigrations   bool
	Schema           = "public"
	LogLevel         = "info"
	HttpEndpoint     = "http://localhost:8080/db"
)

func Flags(flags *pflag.FlagSet) {
	flags.StringVar(&ConnectionString, "db", "DB_URL", "Connection string for the postgres database")
	flags.StringVar(&Schema, "db-schema", "public", "")
	flags.StringVar(&LogLevel, "db-log-level", "info", "")
	flags.StringVar(&PostgRESTJWTSecret, "postgrest-jwt-secret", "PGRST_JWT_SECRET", "JWT Secret Token for PostgREST")
	flags.BoolVar(&skipMigrations, "skip-migrations", false, "Run database migrations")
}

var Pool *pgxpool.Pool
var Gorm *gorm.DB

func readFromEnv(v string) string {
	val := os.Getenv(v)
	if val != "" {
		return val
	}
	return v
}

func Init(connection string) error {
	ConnectionString = readFromEnv(connection)
	Schema = readFromEnv(Schema)
	LogLevel = readFromEnv(LogLevel)
	PostgRESTJWTSecret = readFromEnv(PostgRESTJWTSecret)

	var err error
	Pool, err = duty.NewPgxPool(ConnectionString)
	if err != nil {
		return fmt.Errorf("error creating pgx pool: %w", err)
	}
	conn, err := Pool.Acquire(context.Background())
	if err != nil {
		return fmt.Errorf("error acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := conn.Ping(context.Background()); err != nil {
		return fmt.Errorf("error pinging database: %w", err)
	}
	Gorm, err = duty.NewGorm(ConnectionString, duty.DefaultGormConfig())
	if err != nil {
		return fmt.Errorf("error creating gorm: %w", err)
	}

	if !skipMigrations {
		opts := &migrate.MigrateOptions{}
		if !api.UpstreamConf.Valid() {
			opts.IgnoreFiles = append(opts.IgnoreFiles, "012_changelog.sql")
		}
		if err = duty.Migrate(ConnectionString, opts); err != nil {
			return err
		}
	}

	system := api.Person{}
	if err = Gorm.Find(&system, "name = ?", "System").Error; err != nil {
		return err
	}
	api.SystemUserID = &system.ID
	logger.Infof("System user ID: %s", system.ID.String())
	return nil
}

func GetDB(connnection string) (*sql.DB, error) {
	return duty.NewDB(connnection)
}
