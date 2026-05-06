package main

import (
	"context"
	"fmt"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

func (p *PostgresPlugin) databasesList(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID, "")
	if err != nil {
		return nil, err
	}
	if r.BoundDatabase != "" {
		return []string{r.BoundDatabase}, nil
	}
	var names []string
	if err := r.DB.WithContext(ctx).Raw(`
SELECT datname
FROM pg_database
WHERE datallowconn AND NOT datistemplate
ORDER BY datname
`).Scan(&names).Error; err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	return names, nil
}
