package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqldefrag"
)

type DefragInstallParams struct {
	Source              string `json:"source,omitempty"`
	MaintenanceDatabase string `json:"maintenanceDatabase,omitempty"`
}

func (p *SQLServerPlugin) defragInstall(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params DefragInstallParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	db := r.DB
	return sqldefrag.Install(ctx, db, sqldefrag.InstallOptions{
		Source:              params.Source,
		MaintenanceDatabase: params.MaintenanceDatabase,
	})
}

func (p *SQLServerPlugin) defragRun(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var opts sqldefrag.RunOptions
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &opts); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	db := r.DB
	return p.defragJobs.StartWithDB(db, opts)
}

func (p *SQLServerPlugin) defragStatus(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var opts sqldefrag.StatusOptions
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &opts); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	db := r.DB
	return sqldefrag.GetStatus(ctx, db, opts)
}

func (p *SQLServerPlugin) defragStats(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var opts sqldefrag.StatsOptions
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &opts); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	db := r.DB
	return sqldefrag.Stats(ctx, db, opts)
}

func (p *SQLServerPlugin) defragHistory(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var opts sqldefrag.HistoryOptions
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &opts); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	db := r.DB
	return sqldefrag.History(ctx, db, opts)
}

func (p *SQLServerPlugin) defragSessions(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var opts sqldefrag.StopOptions
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &opts); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	db := r.DB
	return sqldefrag.ListRunningRuns(ctx, db, opts)
}

func (p *SQLServerPlugin) defragTerminate(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var opts sqldefrag.StopOptions
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &opts); err != nil {
			return nil, err
		}
	}
	r, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	db := r.DB
	return sqldefrag.TerminateExistingRuns(ctx, db, opts)
}

func (p *SQLServerPlugin) defragJobsList(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	return p.defragJobs.List(), nil
}

func (p *SQLServerPlugin) defragStop(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	return p.defragJobs.StopRunning(), nil
}
