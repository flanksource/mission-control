package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/postgres/internal/pgstats"
)

type StatsParams struct {
	Database string `json:"database,omitempty"`
}

func (p *PostgresPlugin) stats(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params StatsParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, params.Database)
	if err != nil {
		return nil, err
	}
	return pgstats.Collect(ctx, r.DB)
}
