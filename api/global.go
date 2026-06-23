package api

import (
	"github.com/google/uuid"
)

var (
	BuildVersion string
	BuildCommit  string

	SystemUserID      *uuid.UUID
	CanaryCheckerPath string
	ApmHubPath        string
	ConfigDB          string
	Namespace         string

	// Full URL of the mission control web UI.
	FrontendURL string

	// Full URL of the mission contorl backend.
	PublicURL string

	// DefaultArtifactConnection is the connection that's used to save all playbook artifacts.
	DefaultArtifactConnection string

	// DefaultLLMConnection is the connection that's used as the default LLM provider.
	DefaultLLMConnection string

	DisableOperators bool

	// UpstreamGRPCPort is the port used by agents to call upstream plugin HostService gRPC.
	UpstreamGRPCPort = 8081

	// RemotePluginHostGRPCAddress is the default address Mission Control advertises
	// to remote plugins (spec.address) so they can dial the HostService back-channel
	// for connection resolution and other callbacks. Set with --plugin-host-grpc-address;
	// a plugin can override it per-spec with spec.hostGRPCAddress. It must resolve to
	// this host's UpstreamGRPCPort from wherever the remote plugins run. Defaults to
	// loopback, which only works for plugins running on the same host.
	RemotePluginHostGRPCAddress = "127.0.0.1:8081"

	// PluginHostTLSCertFile / PluginHostTLSKeyFile, when both set, make the plugin
	// HostService gRPC server (UpstreamGRPCPort) serve over TLS. Required before
	// remote plugins are run off-host, since the back-channel carries resolved
	// connection secrets.
	PluginHostTLSCertFile string
	PluginHostTLSKeyFile  string

	// PluginHostTLSCAFile is the PEM CA bundle advertised to remote plugins so
	// they can verify the HostService's TLS certificate. When empty, plugins fall
	// back to the system root CAs.
	PluginHostTLSCAFile string

	// PluginHostTLSClientCAFile, when set, makes the plugin HostService gRPC
	// server require and verify remote plugins' client certificates against this
	// CA bundle (mTLS).
	PluginHostTLSClientCAFile string

	// PluginHostClientCertFile / PluginHostClientKeyFile are the client
	// certificate Mission Control presents when dialing a remote plugin's gRPC
	// server (for plugins that require mTLS via spec.address).
	PluginHostClientCertFile string
	PluginHostClientKeyFile  string
)

const (
	PropertyIncidentsDisabled = "incidents.disable"
)

type LLMBackend string

const (
	LLMBackendAnthropic LLMBackend = "anthropic"
	LLMBackendOpenAI    LLMBackend = "openai"
	LLMBackendOllama    LLMBackend = "ollama"
	LLMBackendGemini    LLMBackend = "gemini"
	LLMBackendBedrock   LLMBackend = "bedrock"
)
