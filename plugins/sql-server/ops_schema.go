package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqlschema"
)

// SchemaParams is the input shape for the `schema` operation. Database
// optionally narrows the scan to a single DB on the instance — when the
// catalog item is itself an MSSQL::Database, BoundDatabase wins.
type SchemaParams struct {
	Database string `json:"database,omitempty"`
}

func (p *SQLServerPlugin) schema(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params SchemaParams
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
	return sqlschema.Introspect(ctx, r.DB, sqlschema.Options{Database: database})
}
