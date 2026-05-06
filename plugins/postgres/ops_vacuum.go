package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/postgres/internal/pgslow"
	"github.com/flanksource/incident-commander/plugins/postgres/internal/pgvacuum"
)

type LimitParams struct {
	Database string `json:"database,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (p *PostgresPlugin) vacuumStats(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params LimitParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, params.Database)
	if err != nil {
		return nil, err
	}
	return pgvacuum.List(ctx, r.DB, params.Limit)
}

func (p *PostgresPlugin) slowQueries(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params LimitParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, params.Database)
	if err != nil {
		return nil, err
	}
	return pgslow.List(ctx, r.DB, params.Limit)
}

func (p *PostgresPlugin) slowQueriesInstall(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params LimitParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, params.Database)
	if err != nil {
		return nil, err
	}
	return pgslow.Install(ctx, r.DB)
}
