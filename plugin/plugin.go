package plugin

// LifecycleKind describes who owns the plugin process lifecycle.
type LifecycleKind string

const (
	// LifecycleManaged means Mission Control starts and stops the plugin process.
	LifecycleManaged LifecycleKind = "managed"

	// LifecycleExternal means something outside Mission Control owns the plugin process.
	LifecycleExternal LifecycleKind = "external"
)

// ConnectionKind describes how Mission Control reaches the plugin.
type ConnectionKind string

const (
	// ConnectionLocal means the plugin is reachable through a local process
	// supervised by Mission Control.
	ConnectionLocal ConnectionKind = "local"

	// ConnectionOutbound means the plugin runs remotely and Mission Control
	// can dial its gRPC/HTTP endpoints directly.
	ConnectionOutbound ConnectionKind = "outbound"

	// ConnectionInbound means the plugin runs remotely and Mission Control
	// cannot dial it directly. The remote side connects to Mission Control,
	// and that connection is multiplexed so Mission Control can initiate
	// plugin gRPC/HTTP requests over it.
	ConnectionInbound ConnectionKind = "inbound"
)
