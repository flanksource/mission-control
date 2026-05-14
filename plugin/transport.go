package plugin

// InvocationTokenGRPCMetadataKey is the gRPC metadata key used to pass the
// short-lived plugin invocation JWT between Mission Control and plugin RPCs.
const InvocationTokenGRPCMetadataKey = "x-flanksource-plugin-invocation"

// InvocationTokenHTTPHeader is the HTTP header used to pass the short-lived
// plugin invocation JWT from Mission Control to plugin HTTP operations.
const InvocationTokenHTTPHeader = "X-Flanksource-Plugin-Invocation"
