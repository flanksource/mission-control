package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

type ConnectionStatus struct {
	Idle    int `json:"Idle"`
	Active  int `json:"Active"`
	Unknown int `json:"Unknown"`
}

func (p *PostgresPlugin) connectionStatus(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	db, err := openPostgres(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := db.QueryContext(queryCtx, `
		SELECT COALESCE(state, 'unknown') AS state, count(*)::int AS connections
		FROM pg_stat_activity
		WHERE pid <> pg_backend_pid()
		GROUP BY COALESCE(state, 'unknown')
	`)
	if err != nil {
		return nil, fmt.Errorf("query pg_stat_activity: %w", err)
	}
	defer rows.Close()

	var status ConnectionStatus
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan pg_stat_activity: %w", err)
		}
		switch strings.ToLower(state) {
		case "idle":
			status.Idle += count
		case "active":
			status.Active += count
		default:
			status.Unknown += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pg_stat_activity: %w", err)
	}

	return status, nil
}
