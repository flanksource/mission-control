package db

import (
	"database/sql"
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
	ConnectionString  string
	skipMigrations    bool
	Schema            = "public"
	postgrestLogLevel = "info"
	HttpEndpoint      = "http://localhost:8080/db"
)

func Flags(flags *pflag.FlagSet) {
	flags.StringVar(&ConnectionString, "db", "DB_URL", "Connection string for the postgres database")
	flags.StringVar(&Schema, "db-schema", "public", "Postgres schema")
	flags.StringVar(&postgrestLogLevel, "postgrest-log-level", "info", "PostgREST log level")
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
	postgrestLogLevel = readFromEnv(postgrestLogLevel)
	PostgRESTJWTSecret = readFromEnv(PostgRESTJWTSecret)

	opts := &migrate.MigrateOptions{
		Skip: skipMigrations,
	}
	if !api.UpstreamConf.Valid() {
		opts.IgnoreFiles = append(opts.IgnoreFiles, "012_changelog.sql")
	}

	ctx, err := duty.InitDB(ConnectionString, opts)
	if err != nil {
		return err
	}

	Pool = ctx.Pool()
	Gorm = ctx.DB()

	system := api.Person{}
	if err := ctx.DB().Find(&system, "name = ?", "System").Error; err != nil {
		return err
	}
	api.SystemUserID = &system.ID
	logger.Infof("System user ID: %s", system.ID.String())

	return nil
}

func GetDB(connnection string) (*sql.DB, error) {
	return duty.NewDB(connnection)
}
