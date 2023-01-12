package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/functions"
	"github.com/flanksource/duty/schema"
	_ "github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/jackc/pgx/v4/log/logrusadapter"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/jackc/pgx/v4/stdlib"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"
)

var ConnectionString string
var Schema = "public"
var LogLevel = "info"
var HttpEndpoint = "http://localhost:8080/db"

func Flags(flags *pflag.FlagSet) {
	flags.StringVar(&ConnectionString, "db", "DB_URL", "Connection string for the postgres database")
	flags.StringVar(&Schema, "db-schema", "public", "")
	flags.StringVar(&LogLevel, "db-log-level", "info", "")
}

//go:embed migrations/*.sql
var embedMigrations embed.FS

//go:embed migrations/_always/*.sql
var embedScripts embed.FS

//go:embed migrations/before/*.sql
var beforeScripts embed.FS

var Pool *pgxpool.Pool
var Gorm *gorm.DB
var pgxConnectionString string

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

	config, err := pgxpool.ParseConfig(ConnectionString)
	if err != nil {
		return err
	}

	if logger.IsTraceEnabled() {
		logrusLogger := &logrus.Logger{
			Out:          os.Stderr,
			Formatter:    new(logrus.TextFormatter),
			Hooks:        make(logrus.LevelHooks),
			Level:        logrus.DebugLevel,
			ExitFunc:     os.Exit,
			ReportCaller: false,
		}
		config.ConnConfig.Logger = logrusadapter.NewLogger(logrusLogger)
	}
	if Pool, err = pgxpool.ConnectConfig(context.Background(), config); err != nil {
		return err
	}

	row := Pool.QueryRow(context.TODO(), "SELECT pg_size_pretty(pg_database_size($1));", config.ConnConfig.Database)
	var size string
	if err := row.Scan(&size); err != nil {
		return err
	}
	logger.Infof("Initialized Incident Commander DB: %s (%s)", config.ConnString(), size)

	pgxConnectionString = stdlib.RegisterConnConfig(config.ConnConfig)

	db, err := GetDB()
	if err != nil {
		return err
	}

	logConfig := glogger.Config{
		SlowThreshold:             time.Second,   // Slow SQL threshold
		LogLevel:                  glogger.Error, // Log level
		IgnoreRecordNotFoundError: true,          // Ignore ErrRecordNotFound error for logger
	}

	if logger.IsDebugEnabled() {
		logConfig.LogLevel = glogger.Warn
	}
	if logger.IsTraceEnabled() {
		logConfig.LogLevel = glogger.Info
	}

	for Gorm, err = gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		FullSaveAssociations: true,
		Logger: glogger.New(
			log.New(os.Stderr, "\r\n", log.LstdFlags), // io writer
			logConfig),
	}); err != nil; {
		return err
	}

	if err = Migrate(); err != nil {
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

func Migrate() error {
	funcs, err := functions.GetFunctions()
	if err != nil {
		return err
	}

	for file, script := range funcs {
		logger.Debugf("Running script %s", file)
		if _, err := Pool.Exec(context.TODO(), script); err != nil {
			return errors.Wrapf(err, "failed to run script %s", file)
		}
	}
	logger.Debugf("Applying schema migrations")
	if err := schema.Apply(context.TODO(), ConnectionString); err != nil {
		return err
	}

	if err := runScripts(embedScripts, "migrations/_always"); err != nil {
		return err
	}

	return nil
}

func runScripts(fs embed.FS, dir string) error {
	scripts, _ := fs.ReadDir(dir)

	if len(scripts) == 0 {
		return fmt.Errorf("No scripts found in %s", dir)
	}

	for _, file := range scripts {
		logger.Debugf("Running script %s", file.Name())
		script, err := fs.ReadFile(dir + "/" + file.Name())
		if err != nil {
			return err
		}
		if _, err := Pool.Exec(context.TODO(), string(script)); err != nil {
			return errors.Wrapf(err, "failed to run script %s", file.Name())
		}
	}

	return nil

}

func GetDB() (*sql.DB, error) {
	return sql.Open("pgx", pgxConnectionString)
}
