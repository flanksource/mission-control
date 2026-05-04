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
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"

	dutyContext "github.com/flanksource/duty/context"
	dutylogs "github.com/flanksource/duty/logs"
	v1 "github.com/flanksource/incident-commander/api/v1"
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
	// Suffix the UI bundle checksum onto the version so the host can detect
	// a UI rebuild and the iframe URL the frontend constructs changes
	// (`?config_id=…&_v=<sha>`), forcing browsers to bypass any stale cache.
	return &pluginpb.PluginManifest{
		Name:         "kubernetes-logs",
		Version:      sdk.FormatVersion(Version, BuildDate, uiChecksum),
		Description:  "Stream logs from a Pod or any of its workload ancestors (Deployment / StatefulSet / DaemonSet / Job).",
		Capabilities: []string{"tabs", "operations"},
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
	}
}

// HTTPHandler powers the iframe UI: GET /api/pods?config_id=… and
// GET /api/logs?pod=…&namespace=…&tailLines=N stream-as-NDJSON.
func (p *KubernetesLogsPlugin) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/pods", p.httpPods)
	mux.HandleFunc("/logs", p.httpLogs)
	mux.Handle("/version", sdk.VersionHandler(sdk.BuildInfo{
		Name:       "kubernetes-logs",
		Version:    Version,
		BuildDate:  BuildDate,
		UIChecksum: uiChecksum,
	}))
	return mux
}

// TailParams is the input shape for the `tail` operation. The CLI sends
// these as JSON via --param/--json; the iframe sends them through the
// `tail` button. PostProcess mirrors the playbook `logs` action shape so
// CEL match expressions and dedup/window settings work the same way.
type TailParams struct {
	Container   string             `json:"container,omitempty"`
	TailLines   int64              `json:"tailLines,omitempty"`
	Previous    bool               `json:"previous,omitempty"`
	PostProcess v1.LogsPostProcess `json:"postProcess,omitempty"`
}

func (p *KubernetesLogsPlugin) tail(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params TailParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
	}
	if params.TailLines <= 0 {
		params.TailLines = 200
	}

	cli, err := p.clients.For(ctx, req.Host)
	if err != nil {
		return nil, err
	}

	pods, err := resolvePods(ctx, cli, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}

	var lines []*dutylogs.LogLine
	for _, pod := range pods {
		podLines, err := tailPod(ctx, cli, pod, params)
		if err != nil {
			lines = append(lines, errorLine(pod, "", err.Error()))
			continue
		}
		lines = append(lines, podLines...)
	}

	dctx := dutyContext.NewContext(ctx)
	return postProcessLogs(dctx, lines, params.PostProcess), nil
}

func (p *KubernetesLogsPlugin) listPods(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	cli, err := p.clients.For(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	pods, err := resolvePods(ctx, cli, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	type Row struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Phase     string `json:"phase"`
		OwnedBy   string `json:"ownedBy,omitempty"`
	}
	out := make([]Row, 0, len(pods))
	for _, pod := range pods {
		owned := ""
		if len(pod.OwnerReferences) > 0 {
			owned = fmt.Sprintf("%s/%s", pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name)
		}
		out = append(out, Row{
			Namespace: pod.Namespace,
			Pod:       pod.Name,
			Phase:     string(pod.Status.Phase),
			OwnedBy:   owned,
		})
	}
	return out, nil
}
