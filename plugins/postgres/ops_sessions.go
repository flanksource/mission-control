package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/postgres/internal/pgsessions"
)

type SessionsListParams struct {
	Database    string `json:"database,omitempty"`
	IncludeIdle bool   `json:"includeIdle,omitempty"`
}

func (p *PostgresPlugin) sessionsList(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionsListParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, params.Database)
	if err != nil {
		return nil, err
	}
	database := params.Database
	if r.BoundDatabase != "" {
		database = r.BoundDatabase
	}
	return pgsessions.List(ctx, r.DB, database, params.IncludeIdle)
}

type SessionActionParams struct {
	PID int `json:"pid"`
}

type SessionActionResult struct {
	PID int  `json:"pid"`
	OK  bool `json:"ok"`
}

func (p *PostgresPlugin) sessionCancel(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionActionParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, err
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, "")
	if err != nil {
		return nil, err
	}
	ok, err := pgsessions.Cancel(ctx, r.DB, params.PID)
	if err != nil {
		return nil, err
	}
	return SessionActionResult{PID: params.PID, OK: ok}, nil
}

func (p *PostgresPlugin) sessionTerminate(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionActionParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, err
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, "")
	if err != nil {
		return nil, err
	}
	ok, err := pgsessions.Terminate(ctx, r.DB, params.PID)
	if err != nil {
		return nil, err
	}
	return SessionActionResult{PID: params.PID, OK: ok}, nil
}
