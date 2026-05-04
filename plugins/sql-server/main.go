// SQL Server plugin: stats / trace / defrag / console / processes
// diagnostics surfaced as a Mission Control plugin.
//
// Build: task build:plugin:sql-server
// Apply: kubectl apply -f plugins/sql-server/Plugin.yaml
package main

import (
	"context"
	"embed"
	"io/fs"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqldefrag"
	"github.com/flanksource/incident-commander/plugins/sql-server/internal/sqltrace"
)

// Operation names — exported as constants so the frontend's API client can
// reference them by symbol when we generate ts bindings.
const (
	OpStats           = "stats"
	OpQuery           = "query"
	OpExplain         = "explain"
	OpSchema          = "schema"
	OpDatabasesList   = "databases-list"
	OpProcessesList   = "processes-list"
	OpProcessKill     = "process-kill"
	OpTraceStart      = "trace-start"
	OpTraceList       = "trace-list"
	OpTraceGet        = "trace-get"
	OpTraceStop       = "trace-stop"
	OpTraceDelete     = "trace-delete"
	OpDefragInstall   = "defrag-install"
	OpDefragRun       = "defrag-run"
	OpDefragStatus    = "defrag-status"
	OpDefragStats     = "defrag-stats"
	OpDefragHistory   = "defrag-history"
	OpDefragSessions  = "defrag-sessions"
	OpDefragTerminate = "defrag-terminate"
	OpDefragJobs      = "defrag-jobs"
	OpDefragStop      = "defrag-stop"
)

//go:generate go run ./internal/gen-checksum

//go:embed ui/*
var uiAssets embed.FS

// Version and BuildDate are injected at link time:
//
//	go build -ldflags "-X main.Version=$VERSION -X 'main.BuildDate=$DATE'" ./plugins/sql-server
//
// The Taskfile (build:plugin:sql-server) sets both. Leaving them at "dev"
// causes the SDK's RegisterPlugin to fail fast — every plugin MUST ship
// with a real version.
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

type SQLServerPlugin struct {
	clients    connectionCache
	traces     *sqltrace.Registry
	defragJobs *sqldefrag.JobRegistry
}

func newPlugin() *SQLServerPlugin {
	return &SQLServerPlugin{
		traces:     sqltrace.NewRegistry(),
		defragJobs: sqldefrag.NewJobRegistry(nil),
	}
}

func (p *SQLServerPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         "sql-server",
		Version:      sdk.FormatVersion(Version, BuildDate, uiChecksum),
		Description:  "Inspect SQL Server health (stats, processes), capture Extended Events traces, run AdaptiveIndexDefrag, and execute ad-hoc queries.",
		Capabilities: []string{"tabs", "operations"},
		Tabs: []*pluginpb.TabSpec{
			{Name: "SQL Server", Icon: "lucide:database", Path: "/", Scope: "config"},
		},
		Operations: operationDefs(),
	}
}

func (p *SQLServerPlugin) Configure(_ context.Context, _ map[string]any) error {
	return nil
}

func (p *SQLServerPlugin) Operations() []sdk.Operation {
	defs := operationDefs()
	handlers := map[string]func(context.Context, sdk.InvokeCtx) (any, error){
		OpStats:           p.stats,
		OpQuery:           p.query,
		OpExplain:         p.explain,
		OpSchema:          p.schema,
		OpDatabasesList:   p.databasesList,
		OpProcessesList:   p.processesList,
		OpProcessKill:     p.processKill,
		OpTraceStart:      p.traceStart,
		OpTraceList:       p.traceList,
		OpTraceGet:        p.traceGet,
		OpTraceStop:       p.traceStop,
		OpTraceDelete:     p.traceDelete,
		OpDefragInstall:   p.defragInstall,
		OpDefragRun:       p.defragRun,
		OpDefragStatus:    p.defragStatus,
		OpDefragStats:     p.defragStats,
		OpDefragHistory:   p.defragHistory,
		OpDefragSessions:  p.defragSessions,
		OpDefragTerminate: p.defragTerminate,
		OpDefragJobs:      p.defragJobsList,
		OpDefragStop:      p.defragStop,
	}
	out := make([]sdk.Operation, 0, len(defs))
	for _, d := range defs {
		h, ok := handlers[d.Name]
		if !ok {
			continue
		}
		out = append(out, sdk.Operation{Def: d, Handler: h})
	}
	return out
}

func operationDefs() []*pluginpb.OperationDef {
	mk := func(name, desc string) *pluginpb.OperationDef {
		return &pluginpb.OperationDef{Name: name, Description: desc, Scope: "config", ResultMime: sdk.ClickyResultMimeType}
	}
	return []*pluginpb.OperationDef{
		mk(OpStats, "Snapshot of SQL Server instance/CPU/memory/disk/IO health."),
		mk(OpQuery, "Execute an ad-hoc SQL statement and return rows + columns."),
		mk(OpExplain, "Return SHOWPLAN (XML or text) for the given statement."),
		mk(OpSchema, "List tables and columns in the database (powers Console autocomplete)."),
		mk(OpDatabasesList, "List ONLINE databases on the instance."),
		mk(OpProcessesList, "List active user sessions on the instance (sp_who2 style)."),
		mk(OpProcessKill, "KILL the given SPID. Not recoverable."),
		mk(OpTraceStart, "Start an Extended Events trace and return the trace handle."),
		mk(OpTraceList, "List active and recently-stopped traces."),
		mk(OpTraceGet, "Fetch a trace's buffered events. Pass {since:<lastKey>} to tail incrementally."),
		mk(OpTraceStop, "Stop a running trace. Returns the final TraceResult."),
		mk(OpTraceDelete, "Stop and remove a trace from the registry."),
		mk(OpDefragInstall, "Install Microsoft TigerToolbox AdaptiveIndexDefrag into the maintenance DB."),
		mk(OpDefragRun, "Run AdaptiveIndexDefrag asynchronously. Returns a job handle."),
		mk(OpDefragStatus, "Read AdaptiveIndexDefrag installation/configuration status."),
		mk(OpDefragStats, "Read fragmentation stats from the dba_indexDefragLog tables."),
		mk(OpDefragHistory, "List recent AdaptiveIndexDefrag history rows."),
		mk(OpDefragSessions, "List currently-running AdaptiveIndexDefrag sessions on the instance."),
		mk(OpDefragTerminate, "KILL existing AdaptiveIndexDefrag sessions on the instance."),
		mk(OpDefragJobs, "List defrag jobs the plugin has started."),
		mk(OpDefragStop, "Stop all running defrag jobs (this plugin process only)."),
	}
}
