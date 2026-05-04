package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqlstats"
)

// StatsParams is the input shape for the `stats` operation.
type StatsParams struct {
	Refresh bool `json:"refresh,omitempty"`
}

func (p *SQLServerPlugin) stats(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params StatsParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	return sqlstats.Collect(ctx, r.DB, params.Refresh)
}
