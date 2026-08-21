// Package clientapi defines the data exchanged between lightweight clients
// and Mission Control. It intentionally contains no transport, persistence,
// rendering, or server implementation dependencies.
package clientapi

// PluginRPCListing is one plugin returned by the client-facing RPC listing.
type PluginRPCListing struct {
	Name    string        `json:"name"`
	Service PluginService `json:"service"`
}

// PluginService describes the operations exposed by a plugin.
type PluginService struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Operations  []PluginOperation `json:"operations"`
}

// PluginOperation describes one remotely invokable plugin operation.
type PluginOperation struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  []PluginParameter `json:"parameters"`
	Schema      Schema            `json:"schema"`
	Path        string            `json:"path,omitempty"`
	Method      string            `json:"method,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

// PluginParameter describes one operation input.
type PluginParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Default     any    `json:"default,omitempty"`
	In          string `json:"in,omitempty"`
}

// Schema describes the JSON object accepted by an operation.
type Schema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

// Property describes one field in an operation schema.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}
