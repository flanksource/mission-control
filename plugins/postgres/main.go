// Postgres plugin: managed-safe diagnostics, query console, sessions,
// locks, and table maintenance visibility surfaced as a Mission Control plugin.
//
// Build: task build:plugin:postgres
// Apply: kubectl apply -f plugins/postgres/Plugin.yaml
package main

import (
	"context"
	"embed"
	"io/fs"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

const (
	OpStats              = "stats"
	OpQuery              = "query"
	OpExplain            = "explain"
	OpSchema             = "schema"
	OpDatabasesList      = "databases-list"
	OpSessionsList       = "sessions-list"
	OpSessionCancel      = "session-cancel"
	OpSessionTerminate   = "session-terminate"
	OpLocksList          = "locks-list"
	OpSlowQueries        = "slow-queries"
	OpSlowQueriesInstall = "slow-queries-install"
	OpVacuumStats        = "vacuum-stats"
)

//go:generate go run ./internal/gen-checksum

//go:embed ui/*
var uiAssets embed.FS

var (
	Version   = ""
	BuildDate = ""
)

func main() {
	sub, err := fs.Sub(uiAssets, "ui")
	if err != nil {
		panic(err)
	}
	sdk.Serve(newPlugin(), sdk.WithStaticAssets(sub))
}

type PostgresPlugin struct {
	clients connectionCache
}

func newPlugin() *PostgresPlugin {
	return &PostgresPlugin{}
}

func (p *PostgresPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         "postgres",
		Version:      sdk.FormatVersion(Version, BuildDate, uiChecksum),
		Description:  "Inspect Postgres health, sessions, locks, vacuum state, slow queries, and run ad-hoc SQL against managed-safe Postgres deployments.",
		Capabilities: []string{"tabs", "operations"},
		Tabs: []*pluginpb.TabSpec{
			{Name: "Postgres", Icon: "lucide:database", Path: "/", Scope: "config"},
		},
		Operations: operationDefs(),
	}
}

func (p *PostgresPlugin) Configure(_ context.Context, _ map[string]any) error {
	return nil
}

func (p *PostgresPlugin) Operations() []sdk.Operation {
	defs := operationDefs()
	handlers := map[string]func(context.Context, sdk.InvokeCtx) (any, error){
		OpStats:              p.stats,
		OpQuery:              p.query,
		OpExplain:            p.explain,
		OpSchema:             p.schema,
		OpDatabasesList:      p.databasesList,
		OpSessionsList:       p.sessionsList,
		OpSessionCancel:      p.sessionCancel,
		OpSessionTerminate:   p.sessionTerminate,
		OpLocksList:          p.locksList,
		OpSlowQueries:        p.slowQueries,
		OpSlowQueriesInstall: p.slowQueriesInstall,
		OpVacuumStats:        p.vacuumStats,
	}
	out := make([]sdk.Operation, 0, len(defs))
	for _, d := range defs {
		if h, ok := handlers[d.Name]; ok {
			out = append(out, sdk.Operation{Def: d, Handler: h})
		}
	}
	return out
}

func operationDefs() []*pluginpb.OperationDef {
	mk := func(name, desc string) *pluginpb.OperationDef {
		return &pluginpb.OperationDef{Name: name, Description: desc, Scope: "config", ResultMime: sdk.ClickyResultMimeType}
	}
	destructive := func(name, desc string) *pluginpb.OperationDef {
		d := mk(name, desc)
		d.Destructive = true
		return d
	}
	return []*pluginpb.OperationDef{
		mk(OpStats, "Managed-safe Postgres instance and database health snapshot."),
		destructive(OpQuery, "Execute an ad-hoc SQL statement and return rows + columns."),
		mk(OpExplain, "Return EXPLAIN (FORMAT JSON) for the given statement without ANALYZE."),
		mk(OpSchema, "List schemas, relations, columns, indexes, and constraints for autocomplete."),
		mk(OpDatabasesList, "List connectable non-template databases."),
		mk(OpSessionsList, "List sessions from pg_stat_activity."),
		destructive(OpSessionCancel, "Cancel the active query for a backend with pg_cancel_backend(pid)."),
		destructive(OpSessionTerminate, "Terminate a backend with pg_terminate_backend(pid)."),
		mk(OpLocksList, "List granted and waiting locks with blocking backend relationships."),
		mk(OpSlowQueries, "List pg_stat_statements rows when the extension is installed."),
		destructive(OpSlowQueriesInstall, "Install pg_stat_statements in the selected database with CREATE EXTENSION IF NOT EXISTS."),
		mk(OpVacuumStats, "List table vacuum/analyze and dead tuple indicators."),
	}
}
