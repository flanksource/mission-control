package plugin

import (
	"context"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

const maxGRPCMessageSize = 64 * 1024 * 1024 // 64MB

// PluginMap is the map of plugin types served by plugin binaries.
// There is exactly one plugin type — the bidirectional mission-control plugin.
var PluginMap = map[string]goplugin.Plugin{
	PluginName: &GRPCPlugin{},
}

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
