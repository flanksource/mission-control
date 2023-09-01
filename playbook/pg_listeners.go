package playbook

import (
	"time"

	"github.com/flanksource/incident-commander/utils"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

func ListenPlaybookPGNotify(db *gorm.DB, pool *pgxpool.Pool) {
	var (
		dbReconnectMaxDuration         = time.Minute
		dbReconnectBackoffBaseDuration = time.Second
	)

	pgNotifyPlaybookSpecUpdated := make(chan string)
	go utils.ListenToPostgresNotify(pool, "playbook_spec_updated", dbReconnectMaxDuration, dbReconnectBackoffBaseDuration, pgNotifyPlaybookSpecUpdated)

	for range pgNotifyPlaybookSpecUpdated {
		clearEventPlaybookCache()
	}
}
