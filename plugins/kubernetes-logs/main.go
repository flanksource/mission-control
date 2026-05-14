// Reference plugin: stream logs from a Kubernetes Pod (or its ancestor —
// Deployment / StatefulSet / DaemonSet / ReplicaSet / Job / CronJob), using
// the Plugin CRD's kubernetes connection.
//
// Build: go build -o $MISSION_CONTROL_PLUGIN_PATH/kubernetes-logs ./plugins/kubernetes-logs
// Apply: kubectl apply -f plugins/kubernetes-logs/Plugin.yaml
package main

import (
	"context"
	"embed"
	"io/fs"
	"net/http"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

//go:generate go run ./internal/gen-checksum

//go:embed all:ui
var uiAssets embed.FS

// Version and BuildDate are injected at link time via the Taskfile's
// build:plugin:kubernetes-logs target. Empty values trip the SDK's
// RegisterPlugin guard, so dev binaries built without ldflags fail loudly.
var (
	Version   = ""
	BuildDate = ""
)

func main() {
	sub, err := fs.Sub(uiAssets, "ui")
	if err != nil {
		panic(err)
	}
	sdk.Serve(&KubernetesLogsPlugin{}, sdk.WithStaticAssets(sub))
}

type KubernetesLogsPlugin struct {
	clients clientCache
}

func (p *KubernetesLogsPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         "kubernetes-logs",
		Version:      sdk.FormatVersion(Version, BuildDate, ""),
		Description:  "Stream logs from a Pod or any of its workload ancestors (Deployment / StatefulSet / DaemonSet / Job).",
		Capabilities: []string{"operations"},
		Tabs: []*pluginpb.TabSpec{
			{Name: "Logs", Icon: "lucide:scroll-text", Path: "/", Scope: "config"},
		},
	}
}

func (p *KubernetesLogsPlugin) Configure(_ context.Context, _ map[string]any) error {
	return nil
}

func (p *KubernetesLogsPlugin) Operations() []sdk.Operation {
	return []sdk.Operation{
		{
			Def: &pluginpb.OperationDef{
				Name:        "tail",
				Description: "Return the last N lines from a Pod (or every Pod owned by a workload).",
				Scope:       "config",
				ResultMime:  sdk.ClickyResultMimeType,
			},
			Handler: p.tail,
		},
		{
			Def: &pluginpb.OperationDef{
				Name:        "list-pods",
				Description: "Resolve a config item to its Pods, walking workload ancestors.",
				Scope:       "config",
				ResultMime:  sdk.ClickyResultMimeType,
			},
			Handler: p.listPods,
		},
		{
			Def: &pluginpb.OperationDef{
				Name:        "logs",
				Description: "Stream Kubernetes pod logs as Server-Sent Events.",
				Scope:       "config",
				ResultMime:  "text/event-stream",
				Streaming:   true,
				Http: []*pluginpb.HTTPBinding{
					{Method: http.MethodGet},
				},
			},
			HTTPHandler: http.HandlerFunc(p.httpLogs),
		},
	}
}
