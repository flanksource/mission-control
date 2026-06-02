package local

import (
	"context"

	"github.com/flanksource/incident-commander/plugin/api"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// PluginServer is the interface that plugin binaries must implement.
// It mirrors the PluginService gRPC service so plugin authors can implement
// it directly without writing the protobuf adapter themselves.
type PluginServer interface {
	api.PluginServiceServer
}

// GRPCPlugin implements goplugin.GRPCPlugin for the bidirectional plugin contract.
type GRPCPlugin struct {
	goplugin.Plugin
	Impl PluginServer // Only set on the plugin side
}

// GRPCServer registers the plugin's PluginServiceServer on the goplugin gRPC server.
func (p *GRPCPlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	api.RegisterPluginServiceServer(s, p.Impl)
	return nil
}

// GRPCClient returns a PluginServiceClient that the host uses to call the plugin.
// The broker is stashed on the client so the host can later request a back-channel
// for the HostService callbacks (the plugin dials the host through the broker).
func (p *GRPCPlugin) GRPCClient(ctx context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &Client{
		Service: api.NewPluginServiceClient(c),
		Broker:  broker,
		Conn:    c,
	}, nil
}

// Client is what goplugin.Client.Dispense returns to the host.
// It wraps the plugin's gRPC client plus the broker for reverse calls.
type Client struct {
	Service api.PluginServiceClient
	Broker  *goplugin.GRPCBroker
	Conn    *grpc.ClientConn
}
