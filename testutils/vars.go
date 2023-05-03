package testutils

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

const (
	TestPostgresPort       = 9879
	TestUpstreamServerPort = 11005
	PGUrl                  = "postgres://postgres:postgres@localhost:9879/test?sslmode=disable"
	UpstreamPGUrl          = "postgres://postgres:postgres@localhost:9879/upstream?sslmode=disable"
)

// Variables used to aid testing.
//
// It's better to fire up a single embedded database instance
// for the entire test suite.
// The variables are here so they can be imported by other packages as well.
var (
	TestDB       *gorm.DB
	TestDBPGPool *pgxpool.Pool

	TestUpstreamDB       *gorm.DB
	TestUpstreamDBPGPool *pgxpool.Pool
)
