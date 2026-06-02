package api

// Kind describes how Mission Control reaches the plugin.
type Kind string

const (
	// PluginKindLocal means the plugin is reachable through a local process
	// supervised by Mission Control.
	PluginKindLocal Kind = "local"

	// PluginKindRemote means the plugin runs remotely and Mission Control
	// can dial its gRPC/HTTP endpoints directly.
	PluginKindRemote Kind = "remote"

	// PluginKindProxied means the plugin runs remotely and is managed by agent.
	// Mission-control proxies all requests to the agent which talks to the plugin.
	//
	// The remote side connects to Mission Control, and that connection is multiplexed so Mission Control can initiate
	// plugin gRPC/HTTP requests over it.
	PluginKindProxied Kind = "proxied"
)

// InvocationTokenGRPCMetadataKey is the gRPC metadata key used to pass the
// short-lived plugin invocation JWT between Mission Control and plugin RPCs.
const InvocationTokenGRPCMetadataKey = "x-flanksource-plugin-invocation"

// InvocationTokenHTTPHeader is the HTTP header used to pass the short-lived
// plugin invocation JWT from Mission Control to plugin HTTP operations.
const InvocationTokenHTTPHeader = "X-Flanksource-Plugin-Invocation"
