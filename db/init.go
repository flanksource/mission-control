package db

import (
	"database/sql"

	"github.com/flanksource/duty"
	_ "github.com/flanksource/duty/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

var (
	HttpEndpoint = "http://localhost:8080/db"
)

var Pool *pgxpool.Pool
var Gorm *gorm.DB

func GetDB(connection string) (*sql.DB, error) {
	return duty.NewDB(connection)
}
