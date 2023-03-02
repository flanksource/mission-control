package db

import (
	"context"
	"database/sql"
	"os"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	_ "github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/pflag"
	"gorm.io/gorm"
)

var ConnectionString string
var Schema = "public"
var LogLevel = "info"
var HttpEndpoint = "http://localhost:8080/db"

func Flags(flags *pflag.FlagSet) {
	flags.StringVar(&ConnectionString, "db", "DB_URL", "Connection string for the postgres database")
	flags.StringVar(&Schema, "db-schema", "public", "")
	flags.StringVar(&LogLevel, "db-log-level", "info", "")
	flags.StringVar(&PostgRESTJWTSecret, "postgrest-jwt-secret", "PGRST_JWT_SECRET", "JWT Secret Token for PostgREST")
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
		return err
	}
	conn, err := Pool.Acquire(context.Background())
	if err != nil {
		return err
	}
	defer conn.Release()
	if err := conn.Ping(context.Background()); err != nil {
		return err
	}
	Gorm, err = duty.NewGorm(ConnectionString, duty.DefaultGormConfig())
	if err != nil {
		return err
	}

	if err = duty.Migrate(ConnectionString); err != nil {
		return err
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
