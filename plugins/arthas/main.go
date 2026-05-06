// Arthas plugin: JVM diagnostics for Kubernetes workloads surfaced as a
// Mission Control plugin.
//
// Build: task build:plugin:arthas
// Apply: kubectl apply -f plugins/arthas/Plugin.yaml
package main

import (
	"context"
	"embed"
	"io/fs"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/arthas/internal/arthas"
)

const (
	OpSessionsList  = "sessions-list"
	OpSessionCreate = "session-create"
	OpSessionDelete = "session-delete"
	OpPodsList      = "pods-list"
	OpExec          = "exec"
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

type ArthasPlugin struct {
	clients  clientCache
	sessions *arthas.SessionRegistry
}

func newPlugin() *ArthasPlugin {
	return &ArthasPlugin{sessions: arthas.NewSessionRegistry()}
}

func (p *ArthasPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         "arthas",
		Version:      sdk.FormatVersion(Version, BuildDate, uiChecksum),
		Description:  "Attach Arthas to JVMs running in Kubernetes workloads and inspect threads, memory, MBeans, logging, and the Arthas web console.",
		Capabilities: []string{"tabs", "operations"},
		Tabs: []*pluginpb.TabSpec{
			{Name: "Arthas", Icon: "lucide:bug", Path: "/", Scope: "config"},
		},
		Operations: operationDefs(),
	}
}

func (p *ArthasPlugin) Configure(_ context.Context, _ map[string]any) error {
	return nil
}

func (p *ArthasPlugin) Operations() []sdk.Operation {
	defs := operationDefs()
	handlers := map[string]func(context.Context, sdk.InvokeCtx) (any, error){
		OpSessionsList:  p.sessionsList,
		OpSessionCreate: p.sessionCreate,
		OpSessionDelete: p.sessionDelete,
		OpPodsList:      p.podsList,
		OpExec:          p.exec,
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
	return []*pluginpb.OperationDef{
		mk(OpSessionsList, "List active Arthas sessions in this plugin process."),
		mk(OpSessionCreate, "Attach Arthas to the selected Kubernetes workload or pod."),
		mk(OpSessionDelete, "Stop and remove an Arthas session."),
		mk(OpPodsList, "List ready target pods for the selected Kubernetes workload."),
		mk(OpExec, "Execute one Arthas command through the session HTTP API."),
	}
}
