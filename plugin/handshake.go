// Package plugin defines the handshake configuration shared between
// the mission-control host and plugin processes.
//
// The host launches plugin binaries with the magic-cookie env var set;
// plugins built with the SDK validate the cookie before starting their
// gRPC server. This protects against accidentally executing a plugin
// binary outside of the host (e.g. running a plugin from the shell).
package plugin

import (
	goplugin "github.com/hashicorp/go-plugin"
)

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
