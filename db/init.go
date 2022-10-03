package db

import (
	"context"
	"database/sql"
	"embed"
	"log"
	"os"
	"time"
	"unsafe"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/jackc/pgx/v4/log/logrusadapter"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/jackc/pgx/v4/stdlib"
	jsoniter "github.com/json-iterator/go"
	jsontime "github.com/liamylian/jsontime/v2/v2"
	"github.com/pressly/goose/v3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"
)

const PostgresTimestampFormat = "2006-01-02T15:04:05.999"

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
	jsontime.AddTimeFormatAlias("postgres_timestamp", PostgresTimestampFormat)

	jsoniter.RegisterTypeDecoderFunc("time.Duration", func(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
		t, err := time.ParseDuration(iter.ReadString())
		if err != nil {
			iter.Error = err
			return
		}
		*((*time.Duration)(ptr)) = t
	})

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
	goose.SetTableName("incident_commander_db_version")
	goose.SetBaseFS(embedMigrations)
	db, err := GetDB()
	if err != nil {
		return err
	}
	defer db.Close()

	for {
		err = goose.UpByOne(db, "migrations", goose.WithAllowMissing())
		if err == goose.ErrNoNextVersion {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func GetDB() (*sql.DB, error) {
	return sql.Open("pgx", pgxConnectionString)
}
