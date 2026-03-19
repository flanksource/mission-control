package v1

// MCPMetadata defines metadata for MCP (Model Context Protocol) tool registration.
// These fields control how the playbook or view appears to LLM clients
// such as Claude, Gemini, and Codex.
type MCPMetadata struct {
	// Title overrides the default tool title shown to LLMs.
	Title string `json:"title,omitempty" yaml:"title,omitempty"`

	// Description provides additional context for LLMs beyond the spec description.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// ReadOnlyHint indicates the tool does not modify any state.
	ReadOnlyHint *bool `json:"readOnlyHint,omitempty" yaml:"readOnlyHint,omitempty"`

	// DestructiveHint indicates the tool may perform destructive operations.
	DestructiveHint *bool `json:"destructiveHint,omitempty" yaml:"destructiveHint,omitempty"`

	// IdempotentHint indicates repeated calls with same args have no additional effect.
	IdempotentHint *bool `json:"idempotentHint,omitempty" yaml:"idempotentHint,omitempty"`

	// OpenWorldHint indicates the tool interacts with external entities.
	OpenWorldHint *bool `json:"openWorldHint,omitempty" yaml:"openWorldHint,omitempty"`

	// Tags are keywords for categorizing the tool for LLM discovery.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}
