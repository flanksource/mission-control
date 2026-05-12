package pluginpb

// The JWT used by the plugin to talk to mission-control grpc server
// is injected as a grpc metadata and this is the key for the metadata.
const PluginInvocationTokenMetadataKey = "x-flanksource-plugin-invocation"
