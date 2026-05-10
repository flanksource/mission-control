package sdk

import (
	"fmt"
	"os"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/flanksource/incident-commander/plugin/adapter"
)

// Serve is the entry point for plugin binaries. It starts the go-plugin gRPC
// server and blocks until the host disconnects.
func Serve(impl Plugin) {
	if m := impl.Manifest(); m != nil {
		fmt.Fprintf(os.Stderr, "plugin %s loading: version=%s\n", m.Name, m.Version)
	}

	srv := newPluginServer(impl)

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: adapter.Handshake,
		Plugins: map[string]goplugin.Plugin{
			adapter.PluginName: &grpcAdapter{srv: srv},
		},
		GRPCServer: adapter.GRPCServerFactory,
	})
}
