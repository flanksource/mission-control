package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqltrace"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/xetrace"
)

// TraceStartParams maps to xetrace.CreateOptions plus a Duration. The
// frontend's TraceTab posts this as JSON.
type TraceStartParams struct {
	Database          string `json:"database,omitempty"`
	DurationSeconds   int    `json:"durationSeconds,omitempty"`
	PollSeconds       int    `json:"pollSeconds,omitempty"`
	MinDurationMicros int64  `json:"minDurationMicros,omitempty"`
	MaxMemoryKB       int    `json:"maxMemoryKb,omitempty"`
	MaxEvents         int    `json:"maxEvents,omitempty"`
}

func (p *SQLServerPlugin) traceStart(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params TraceStartParams
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
	return p.traces.Start(ctx, r.DB, sqltrace.StartOptions{
		CreateOptions: xetrace.CreateOptions{
			DatabaseName:      database,
			MinDurationMicros: params.MinDurationMicros,
			Events:            xetrace.DefaultEvents,
			MaxMemoryKB:       params.MaxMemoryKB,
			MaxEvents:         params.MaxEvents,
		},
		ConfigItemID: req.ConfigItemID,
		Duration:     time.Duration(params.DurationSeconds) * time.Second,
		Poll:         time.Duration(params.PollSeconds) * time.Second,
	})
}

func (p *SQLServerPlugin) traceList(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	p.traces.GC()
	return p.traces.List(), nil
}

type TraceIDParams struct {
	ID    string `json:"id"`
	Since string `json:"since,omitempty"`
}

func (p *SQLServerPlugin) traceGet(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params TraceIDParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, err
	}
	t, ok := p.traces.Get(params.ID)
	if !ok {
		return nil, fmt.Errorf("trace %q not found", params.ID)
	}
	events := t.EventsSince(params.Since)
	return map[string]any{
		"id":      t.ID,
		"running": t.Running(),
		"events":  events,
	}, nil
}

func (p *SQLServerPlugin) traceStop(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params TraceIDParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, err
	}
	t, err := p.traces.Stop(params.ID)
	if err != nil {
		return nil, err
	}
	return t.Result(), nil
}

func (p *SQLServerPlugin) traceDelete(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params TraceIDParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, err
	}
	removed, err := p.traces.Delete(params.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"removed": removed}, nil
}
