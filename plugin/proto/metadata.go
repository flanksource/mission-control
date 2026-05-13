package pluginpb

// PluginInvocationTokenMetadataKey is the gRPC metadata key used to pass the
// short-lived plugin invocation JWT between Mission Control and plugin RPCs.
const PluginInvocationTokenMetadataKey = "x-flanksource-plugin-invocation"
