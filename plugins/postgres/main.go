package main

import (
	"context"
	"embed"
	"io/fs"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

//go:embed all:ui
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
	sdk.Serve(&PostgresPlugin{}, sdk.WithStaticAssets(sub))
}

type PostgresPlugin struct{}

func (p *PostgresPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         "postgres",
		Version:      sdk.FormatVersion(Version, BuildDate, ""),
		Description:  "Postgres activity metrics from pg_stat_activity.",
		Capabilities: []string{"operations"},
		Tabs: []*pluginpb.TabSpec{
			{Name: "Postgres", Icon: "lucide:database", Path: "/", Scope: "global"},
		},
	}
}

func (p *PostgresPlugin) Configure(_ context.Context, _ map[string]any) error {
	return nil
}

func (p *PostgresPlugin) Operations() []sdk.Operation {
	return []sdk.Operation{
		{
			Def: &pluginpb.OperationDef{
				Name:        "connection_status",
				Description: "Count Postgres connections by active, idle, and unknown state using pg_stat_activity.",
				Scope:       "global",
				ResultMime:  sdk.ClickyResultMimeType,
			},
			Handler: p.connectionStatus,
		},
	}
}
