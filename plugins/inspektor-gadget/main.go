// Inspektor Gadget plugin: eBPF workload diagnostics for Kubernetes resources
// through the Mission Control plugin runtime.
//
// Build: task -d plugins/inspektor-gadget build
// Apply: kubectl apply -f plugins/inspektor-gadget/Plugin.yaml
package main

import (
	"context"
	"embed"
	"io/fs"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

const (
	OpStatus       = "status"
	OpInstallPlan  = "install-plan"
	OpInstall      = "install"
	OpTracesList   = "traces-list"
	OpTraceStart   = "trace-start"
	OpTraceStop    = "trace-stop"
	OpTraceList    = "trace-list"
	OpTraceEvents  = "trace-events"
	pluginName     = "inspektor-gadget"
	defaultIGTag   = "v0.52.0"
	defaultMaxRuns = 5
)

//go:generate go run ./internal/gen-checksum

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
	sdk.Serve(newPlugin(), sdk.WithStaticAssets(sub))
}

type InspektorGadgetPlugin struct {
	clients  clientCache
	sessions *SessionRegistry
	runner   TraceRunner
	settings PluginSettings
}

type PluginSettings struct {
	GadgetNamespace string
	GadgetTag       string
	MaxDurationSec  int
	MaxEvents       int
	MaxSessions     int
}

func defaultSettings() PluginSettings {
	return PluginSettings{
		GadgetNamespace: "gadget",
		GadgetTag:       defaultIGTag,
		MaxDurationSec:  900,
		MaxEvents:       10000,
		MaxSessions:     defaultMaxRuns,
	}
}

func newPlugin() *InspektorGadgetPlugin {
	settings := defaultSettings()
	return &InspektorGadgetPlugin{
		sessions: NewSessionRegistry(settings.MaxEvents),
		runner:   NewTraceRunner(),
		settings: settings,
	}
}

func (p *InspektorGadgetPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         pluginName,
		Version:      sdk.FormatVersion(Version, BuildDate, uiChecksum),
		Description:  "Run Inspektor Gadget eBPF traces against Kubernetes workloads through the configured Kubernetes API connection.",
		Capabilities: []string{"tabs", "operations"},
		Tabs: []*pluginpb.TabSpec{
			{Name: "Gadget", Icon: "lucide:radar", Path: "/", Scope: "config"},
		},
		Operations: operationDefs(),
	}
}

func (p *InspektorGadgetPlugin) Configure(_ context.Context, settings map[string]any) error {
	next := p.settings
	if v, ok := settings["gadgetNamespace"].(string); ok && v != "" {
		next.GadgetNamespace = v
	}
	if v, ok := settings["gadgetTag"].(string); ok && v != "" {
		next.GadgetTag = v
	}
	if v, ok := numberSetting(settings, "maxDurationSec"); ok && v > 0 {
		next.MaxDurationSec = v
	}
	if v, ok := numberSetting(settings, "maxEvents"); ok && v > 0 {
		next.MaxEvents = v
	}
	if v, ok := numberSetting(settings, "maxSessions"); ok && v > 0 {
		next.MaxSessions = v
	}
	p.settings = next
	p.sessions.SetMaxEvents(next.MaxEvents)
	return nil
}

func (p *InspektorGadgetPlugin) Operations() []sdk.Operation {
	handlers := map[string]func(context.Context, sdk.InvokeCtx) (any, error){
		OpStatus:      p.status,
		OpInstallPlan: p.installPlan,
		OpInstall:     p.install,
		OpTracesList:  p.tracesList,
		OpTraceStart:  p.traceStart,
		OpTraceStop:   p.traceStop,
		OpTraceList:   p.traceList,
		OpTraceEvents: p.traceEvents,
	}
	defs := operationDefs()
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
		mk(OpStatus, "Check Inspektor Gadget deployment readiness through the Kubernetes API."),
		mk(OpInstallPlan, "Return the Kubernetes manifest that would install Inspektor Gadget."),
		mk(OpInstall, "Apply the Inspektor Gadget Kubernetes manifest through the Kubernetes API."),
		mk(OpTracesList, "List supported Inspektor Gadget traces."),
		mk(OpTraceStart, "Start a bounded Inspektor Gadget trace session for this resource."),
		mk(OpTraceStop, "Stop a running trace session."),
		mk(OpTraceList, "List active and recent trace sessions."),
		mk(OpTraceEvents, "Return buffered events for a trace session."),
	}
}

func numberSetting(settings map[string]any, key string) (int, bool) {
	switch v := settings[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}
