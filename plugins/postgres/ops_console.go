package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/postgres/internal/pgquery"
)

type QueryParams struct {
	Statement string `json:"statement"`
	Database  string `json:"database,omitempty"`
	RowLimit  int    `json:"rowLimit,omitempty"`
}

func (p *PostgresPlugin) query(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params QueryParams
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
	return pgquery.Execute(ctx, r.DB, params.Statement, pgquery.Options{
		Database: database,
		RowLimit: params.RowLimit,
	})
}

type ExplainParams struct {
	Statement string `json:"statement"`
	Database  string `json:"database,omitempty"`
}

type ExplainResult struct {
	Plan   string `json:"plan"`
	Format string `json:"format"`
}

func (p *PostgresPlugin) explain(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params ExplainParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, params.Database)
	if err != nil {
		return nil, err
	}
	plan, err := pgquery.Explain(ctx, r.DB, params.Statement)
	if err != nil {
		return nil, err
	}
	return ExplainResult{Plan: plan, Format: "json"}, nil
}
