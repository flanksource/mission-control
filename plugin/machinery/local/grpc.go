package local

import (
	"context"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pluginpb "github.com/flanksource/incident-commander/plugin"
)

const maxGRPCMessageSize = 64 * 1024 * 1024 // 64MB

// PluginServer is the interface that plugin binaries must implement.
// It mirrors the PluginService gRPC service so plugin authors can implement
// it directly without writing the protobuf adapter themselves.
type PluginServer interface {
	pluginpb.PluginServiceServer
}

// GRPCPlugin implements goplugin.GRPCPlugin for the bidirectional plugin contract.
type GRPCPlugin struct {
	goplugin.Plugin
	Impl PluginServer // Only set on the plugin side
}

// GRPCServer registers the plugin's PluginServiceServer on the goplugin gRPC server.
func (p *GRPCPlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	pluginpb.RegisterPluginServiceServer(s, p.Impl)
	return nil
}

// GRPCClient returns a PluginServiceClient that the host uses to call the plugin.
// The broker is stashed on the client so the host can later request a back-channel
// for the HostService callbacks (the plugin dials the host through the broker).
func (p *GRPCPlugin) GRPCClient(ctx context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &Client{
		Service: pluginpb.NewPluginServiceClient(c),
		Broker:  broker,
		Conn:    c,
	}, nil
}

// Client is what goplugin.Client.Dispense returns to the host.
// It wraps the plugin's gRPC client plus the broker for reverse calls.
type Client struct {
	Service pluginpb.PluginServiceClient
	Broker  *goplugin.GRPCBroker
	Conn    *grpc.ClientConn
}

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

// ProtocolVersion is bumped whenever the gRPC contract in plugin/proto/
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
