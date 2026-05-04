// Package sdk is what plugin authors import to build a mission-control plugin.
//
// A minimal plugin looks like:
//
//	func main() {
//	    sdk.Serve(&MyPlugin{}, sdk.WithStaticAssets(uiAssets))
//	}
//
// Plugin authors implement the Plugin interface; the SDK handles the go-plugin
// handshake, gRPC server setup, HTTP server (vite static + /api/*), and the
// reverse channel back to the mission-control host.
package sdk

import (
	"context"
	"net/http"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

// Plugin is the interface plugin authors implement. All methods are called by
// the SDK in response to host RPCs.
type Plugin interface {
	// Manifest returns the plugin's static manifest: name, version, the tabs
	// it wants attached to matching catalog items, and the operations it
	// exposes. Called once on startup in response to RegisterPlugin.
	Manifest() *pluginpb.PluginManifest

	// Configure applies host-pushed configuration. settings is the merged CRD
	// spec.properties + any host-side overrides as a JSON-decoded map.
	Configure(ctx context.Context, settings map[string]any) error

	// Operations returns the runtime handlers for the operations declared in
	// Manifest(). The Def field on each Operation should match a name in
	// Manifest().Operations.
	Operations() []Operation

	// HTTPHandler is mounted at /api/* on the plugin's HTTP server. The host
	// reverse-proxies /api/plugins/<name>/ui/api/* to this handler. Plugins
	// use it to power their own UI (the host neither knows nor cares about
	// these endpoints).
	HTTPHandler() http.Handler
}

// Operation is a runtime handler for a named operation declared in the
// plugin's manifest. Handler returns either a value implementing the clicky
// Result interface (preferred — the SDK marshals to application/clicky+json)
// or raw bytes for advanced cases.
type Operation struct {
	Def     *pluginpb.OperationDef
	Handler func(ctx context.Context, req InvokeCtx) (any, error)
}

// InvokeCtx is passed to operation handlers. It exposes the request payload,
// the calling user's identity/permissions, and a HostClient for callbacks.
type InvokeCtx struct {
	Operation    string
	ParamsJSON   []byte
	ConfigItemID string
	Caller       *pluginpb.CallerContext
	Host         HostClient
}
