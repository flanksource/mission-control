package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqlprocesses"
)

// databasesList returns the names of every ONLINE database on the instance,
// sorted alphabetically. Used by the Processes tab's database picker.
func (p *SQLServerPlugin) databasesList(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	if r.BoundDatabase != "" {
		return []string{r.BoundDatabase}, nil
	}
	var names []string
	if err := r.DB.WithContext(ctx).Raw(
		`SELECT name FROM sys.databases WHERE state_desc = 'ONLINE' ORDER BY name`,
	).Scan(&names).Error; err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	return names, nil
}

type ProcessesListParams struct {
	Database        string `json:"database,omitempty"`
	IncludeSleeping bool   `json:"includeSleeping,omitempty"`
}

func (p *SQLServerPlugin) processesList(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params ProcessesListParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	database := params.Database
	if r.BoundDatabase != "" {
		database = r.BoundDatabase
	}
	return sqlprocesses.List(ctx, r.DB, database, params.IncludeSleeping)
}

type ProcessKillParams struct {
	SessionID int `json:"sessionId"`
}

type ProcessKillResult struct {
	Killed    bool `json:"killed"`
	SessionID int  `json:"sessionId"`
}

func (p *SQLServerPlugin) processKill(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params ProcessKillParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, err
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	if err := sqlprocesses.Kill(ctx, r.DB, params.SessionID); err != nil {
		return nil, err
	}
	return ProcessKillResult{Killed: true, SessionID: params.SessionID}, nil
}
