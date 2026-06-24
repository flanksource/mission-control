package v1

import (
	"github.com/flanksource/duty/types"
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PluginConnectionMappings maps plugin connection requests to Mission Control connections.
type PluginConnectionMappings struct {
	// Types maps a connection type requested by the plugin to the Mission Control
	// connection that should satisfy that request.
	//+kubebuilder:validation:Optional
	Types map[string]string `json:"types,omitempty"`

	// Labels maps plugin-defined connection labels to Mission Control connections.
	//+kubebuilder:validation:Optional
	Labels map[string]string `json:"labels,omitempty"`
}

// PluginSpec configures a binary-backed Mission Control plugin.
//
// A Plugin is a separate process that mission-control supervises and talks
// to over bi-directional gRPC. The plugin can register UI tabs that are
// iframed into the catalog detail page, and operations that are exposed
// over the HTTP API and as CLI subcommands.
//
// +kubebuilder:validation:XValidation:rule="has(self.source) || has(self.address)",message="spec.source is required (or spec.address for a remote plugin)"
type PluginSpec struct {
	// Source is the deps package name or URL the binary is installed from
	// (via flanksource/deps). Mission-control places the resulting binary
	// in $MISSION_CONTROL_PLUGIN_PATH. Required unless Address is set, in
	// which case the plugin is remote and there is no binary to install.
	//+kubebuilder:validation:Optional
	Source string `json:"source,omitempty"`

	// Address, when set, marks this plugin as remote: Mission Control dials the
	// plugin's gRPC server at this network address (host:port) instead of
	// installing and supervising a local binary. Source/Version/Checksum are
	// ignored for remote plugins.
	//+kubebuilder:validation:Optional
	Address string `json:"address,omitempty"`

	// HostGRPCAddress is the address this remote plugin uses to reach Mission
	// Control's HostService back-channel (for connection resolution and other
	// callbacks). Set it per plugin when plugins run in different networks and
	// reach Mission Control at different addresses. When empty, the host's
	// configured default is used. Only meaningful when Address is set.
	//+kubebuilder:validation:Optional
	HostGRPCAddress string `json:"hostGRPCAddress,omitempty"`

	// CACert is an optional PEM CA bundle used to verify the TLS certificate the
	// remote plugin serves at Address. When empty, Mission Control dials the
	// plugin in plaintext (only safe for same-host plugins). Only meaningful when
	// Address is set.
	//+kubebuilder:validation:Optional
	CACert string `json:"caCert,omitempty"`

	// Version of the binary to install. Forwarded to deps.Install verbatim
	// (e.g. a semver tag, "latest", or a git ref the deps package supports).
	Version string `json:"version,omitempty"`

	// Checksum optionally pins the downloaded binary to a known good hash.
	// When set, deps refuses to install a binary with a different checksum.
	Checksum string `json:"checksum,omitempty"`

	// Selector decides which catalog (config) items this plugin's tabs
	// attach to. The same ResourceSelector semantics used by Playbook.Configs
	// apply: filter by config type, labels, tags, agent, namespace, name.
	// An empty selector matches every config item.
	//+kubebuilder:validation:Optional
	Selector types.ResourceSelector `json:"selector,omitempty"`

	// Connections defines mappings from plugin connection requests to
	// Mission Control connections. Types maps connection types such as
	// "sql" or "aws" to a connection URI, while Labels maps plugin-defined
	// labels such as "artifactProd" or "sqlDev" to a connection URI.
	//+kubebuilder:validation:Optional
	Connections PluginConnectionMappings `json:"connections,omitempty"`

	// Audit selects plugin operations whose invocations should be recorded as
	// catalog config changes. Each entry is matched against the operation name
	// using Mission Control match expressions: exact names, wildcards such as
	// "*" or "logs-*", and negations such as "!debug". When omitted or empty,
	// plugin invocations are not recorded as changes.
	//+kubebuilder:validation:Optional
	Audit []string `json:"audit,omitempty"`

	// Properties are arbitrary key/value settings forwarded to the plugin
	// via the Configure() RPC at startup. Use this for plugin-specific
	// configuration that doesn't fit any of the other fields.
	//+kubebuilder:validation:Optional
	Properties map[string]string `json:"properties,omitempty"`
}

// PluginStatus reflects the supervised state of the plugin process.
type PluginStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`

	// InstalledPath is where deps placed the binary on the host filesystem.
	InstalledPath string `json:"installedPath,omitempty" yaml:"installedPath,omitempty"`

	// PluginVersion is the version reported by the plugin in its manifest.
	PluginVersion string `json:"pluginVersion,omitempty" yaml:"pluginVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Plugin is the schema for the Plugins API.
type Plugin struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata" yaml:"metadata"`

	Spec PluginSpec `json:"spec" yaml:"spec"`

	//+kubebuilder:validation:Optional
	Status PluginStatus `json:"status" yaml:"status"`
}

var _ kopper.StatusPatchGenerator = (*Plugin)(nil)
var _ kopper.StatusConditioner = (*Plugin)(nil)
var _ kopper.ObservedGenerationSetter = (*Plugin)(nil)

func (p *Plugin) SetObservedGeneration(generation int64) {
	p.Status.ObservedGeneration = generation
}

func (p *Plugin) GetObservedGeneration() int64 {
	return p.Status.ObservedGeneration
}

func (p *Plugin) GetStatusConditions() *[]metav1.Condition {
	return &p.Status.Conditions
}

func (p *Plugin) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*Plugin)
	if !ok {
		return nil
	}
	if cmp.Diff(p.Status, og.Status) == "" {
		return nil
	}
	clientObj, ok := original.(client.Object)
	if !ok {
		return nil
	}
	return client.MergeFrom(clientObj)
}

func (p *Plugin) GetUUID() (uuid.UUID, error) {
	return uuid.Parse(string(p.UID))
}

// +kubebuilder:object:root=true

// PluginList contains a list of Plugin.
type PluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Plugin `json:"items"`
}
