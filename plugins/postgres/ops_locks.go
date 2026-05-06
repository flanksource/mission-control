package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/postgres/internal/pglocks"
)

type LocksListParams struct {
	Database    string `json:"database,omitempty"`
	OnlyBlocked bool   `json:"onlyBlocked,omitempty"`
}

func (p *PostgresPlugin) locksList(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params LocksListParams
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
	return pglocks.List(ctx, r.DB, database, params.OnlyBlocked)
}
