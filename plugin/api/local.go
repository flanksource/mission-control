package api

import (
	goplugin "github.com/hashicorp/go-plugin"
	grpc "google.golang.org/grpc"
)

const maxGRPCMessageSize = 64 * 1024 * 1024 // 64MB

// ProtocolVersion is bumped whenever the gRPC contract in plugin/plugin.proto
// changes in a non-additive way. Plugins reporting a different version
// are rejected by the supervisor.
const ProtocolVersion = uint(1)

// Handshake is the shared go-plugin handshake config.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   "MISSION_CONTROL_PLUGIN",
	MagicCookieValue: "mission-control-plugin/v1",
}

// PluginName is the key plugins are registered under in the go-plugin
// PluginMap. There is only one plugin per binary.
const PluginName = "mission-control"

// GRPCServerFactory returns a grpc.Server with raised message size limits,
// suitable for plugins that may exchange large config item payloads or
// artifact bytes.
func GRPCServerFactory(opts []grpc.ServerOption) *grpc.Server {
	opts = append(opts,
		grpc.MaxRecvMsgSize(maxGRPCMessageSize),
		grpc.MaxSendMsgSize(maxGRPCMessageSize),
	)
	return grpc.NewServer(opts...)
}
