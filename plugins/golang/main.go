// Golang plugin: Go runtime diagnostics for Kubernetes workloads surfaced as a
// Mission Control plugin.
//
// Build: task build:plugin:golang
// Apply: kubectl apply -f plugins/golang/Plugin.yaml
package main

import (
	"context"
	"embed"
	"io/fs"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

const (
	OpPodsList        = "pods-list"
	OpSessionsList    = "sessions-list"
	OpSessionCreate   = "session-create"
	OpSessionDelete   = "session-delete"
	OpRuntimeSnapshot = "runtime-snapshot"
	OpGoroutines      = "goroutines"
	OpProfileCollect  = "profile-collect"
	OpProfileStart    = "profile-start"
	OpProfileStatus   = "profile-status"
	OpProfileStop     = "profile-stop"
	OpProfileRunsList = "profile-runs-list"
	pluginName        = "golang"
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

type GolangPlugin struct {
	clients  clientCache
	sessions *SessionRegistry
	profiles *ProfileRegistry
	viewers  *ProfileViewerRegistry
	settings PluginSettings
}

type PluginSettings struct {
	DefaultGopsPort  int      `json:"defaultGopsPort,omitempty"`
	DefaultGopsPorts []int    `json:"defaultGopsPorts,omitempty"`
	DefaultPprofPort int      `json:"defaultPprofPort,omitempty"`
	PprofBasePath    string   `json:"pprofBasePath,omitempty"`
	GopsConfigDirs   []string `json:"gopsConfigDirs,omitempty"`
	MaxSessions      int      `json:"maxSessions,omitempty"`
	MaxProfileSec    int      `json:"maxProfileSeconds,omitempty"`
}

func defaultSettings() PluginSettings {
	return PluginSettings{
		DefaultGopsPorts: []int{6061},
		PprofBasePath:    "/debug/pprof",
		GopsConfigDirs:   []string{"/tmp/gops", "/root/.config/gops", "/home/*/.config/gops"},
		MaxSessions:      5,
		MaxProfileSec:    30,
	}
}

func newPlugin() *GolangPlugin {
	viewers := NewProfileViewerRegistry()
	viewers.StartReaper(context.Background())
	return &GolangPlugin{
		sessions: NewSessionRegistry(),
		profiles: NewProfileRegistry(),
		viewers:  viewers,
		settings: defaultSettings(),
	}
}

func (p *GolangPlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         pluginName,
		Version:      sdk.FormatVersion(Version, BuildDate, uiChecksum),
		Description:  "Inspect Go runtime diagnostics from Kubernetes workloads that expose gops and/or pprof on localhost-only ports.",
		Capabilities: []string{"tabs", "operations"},
		Tabs: []*pluginpb.TabSpec{
			{Name: "Golang", Icon: "lucide:activity", Path: "/", Scope: "config"},
		},
		Operations: operationDefs(),
	}
}

func (p *GolangPlugin) Configure(_ context.Context, settings map[string]any) error {
	next := p.settings
	if v, ok := numberSetting(settings, "defaultGopsPort"); ok && v > 0 {
		next.DefaultGopsPort = v
	}
	if v, ok := intSliceSetting(settings, "defaultGopsPorts"); ok {
		next.DefaultGopsPorts = v
	}
	if v, ok := numberSetting(settings, "defaultPprofPort"); ok && v > 0 {
		next.DefaultPprofPort = v
	}
	if v, ok := settings["pprofBasePath"].(string); ok && v != "" {
		next.PprofBasePath = normalizePprofBase(v)
	}
	if v, ok := stringSliceSetting(settings, "gopsConfigDirs"); ok {
		next.GopsConfigDirs = v
	}
	if v, ok := numberSetting(settings, "maxSessions"); ok && v > 0 {
		next.MaxSessions = v
	}
	if v, ok := numberSetting(settings, "maxProfileSeconds"); ok && v > 0 {
		next.MaxProfileSec = v
	}
	p.settings = next
	return nil
}

func (p *GolangPlugin) Operations() []sdk.Operation {
	handlers := map[string]func(context.Context, sdk.InvokeCtx) (any, error){
		OpPodsList:        p.podsList,
		OpSessionsList:    p.sessionsList,
		OpSessionCreate:   p.sessionCreate,
		OpSessionDelete:   p.sessionDelete,
		OpRuntimeSnapshot: p.runtimeSnapshot,
		OpGoroutines:      p.goroutines,
		OpProfileCollect:  p.profileCollect,
		OpProfileStart:    p.profileStart,
		OpProfileStatus:   p.profileStatus,
		OpProfileStop:     p.profileStop,
		OpProfileRunsList: p.profileRunsList,
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
		mk(OpPodsList, "List ready target pods for the selected Kubernetes workload."),
		mk(OpSessionsList, "List active Golang diagnostics sessions in this plugin process."),
		mk(OpSessionCreate, "Open port-forwards to an instrumented Go process exposing gops and/or pprof."),
		mk(OpSessionDelete, "Stop and remove a Golang diagnostics session."),
		mk(OpRuntimeSnapshot, "Read Go version, runtime stats, and memory stats."),
		mk(OpGoroutines, "Read the current goroutine stack dump."),
		mk(OpProfileCollect, "Collect a heap, CPU, or execution trace profile."),
		mk(OpProfileStart, "Start a profile run and track it in process-local history."),
		mk(OpProfileStatus, "Read a profile run status."),
		mk(OpProfileStop, "Stop a running profile run."),
		mk(OpProfileRunsList, "List recent profile runs for a diagnostics session."),
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

func intSliceSetting(settings map[string]any, key string) ([]int, bool) {
	raw, ok := settings[key]
	if !ok {
		return nil, false
	}
	switch v := raw.(type) {
	case []int:
		return uniquePositiveInts(v...), true
	case []any:
		out := make([]int, 0, len(v))
		for _, item := range v {
			switch n := item.(type) {
			case int:
				out = append(out, n)
			case int64:
				out = append(out, int(n))
			case float64:
				out = append(out, int(n))
			}
		}
		return uniquePositiveInts(out...), len(out) > 0
	default:
		return nil, false
	}
}

func stringSliceSetting(settings map[string]any, key string) ([]string, bool) {
	raw, ok := settings[key]
	if !ok {
		return nil, false
	}
	switch v := raw.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}
