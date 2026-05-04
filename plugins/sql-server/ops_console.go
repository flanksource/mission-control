package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqlquery"
)

// QueryParams is the input shape for the `query` operation. The frontend's
// SqlConsole component sends Statement (the editor's contents) plus an
// optional Database (USE <db> first). RowLimit caps the response — the
// frontend's data table can choke on huge result sets.
type QueryParams struct {
	Statement string `json:"statement"`
	Database  string `json:"database,omitempty"`
	RowLimit  int    `json:"rowLimit,omitempty"`
}

func (p *SQLServerPlugin) query(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params QueryParams
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
	return sqlquery.Execute(ctx, r.DB, params.Statement, sqlquery.Options{
		Database: database,
		RowLimit: params.RowLimit,
	})
}

// ExplainParams is the input for the `explain` operation. Format is "xml"
// (default) or "text" — matches SQL Server's SHOWPLAN modes.
type ExplainParams struct {
	Statement string `json:"statement"`
	Database  string `json:"database,omitempty"`
	Format    string `json:"format,omitempty"`
}

type ExplainResult struct {
	Plan   string `json:"plan"`
	Format string `json:"format"`
}

func (p *SQLServerPlugin) explain(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params ExplainParams
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
	plan, err := sqlquery.Explain(ctx, r.DB, params.Statement, database, params.Format)
	if err != nil {
		return nil, err
	}
	format := params.Format
	if format == "" {
		format = "xml"
	}
	return ExplainResult{Plan: plan, Format: format}, nil
}
