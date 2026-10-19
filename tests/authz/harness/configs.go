package harness

import (
	"strings"

	"github.com/google/uuid"
)

// Config is a config item, created through the database API. Configs are scraped in real use; creating one directly
// stands in for a scraper.
type Config struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	ConfigClass string            `json:"config_class"`
	Config      string            `json:"config,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// ScopeType implements Resource.
func (*Config) ScopeType() string { return "config" }

// Prepare implements Resource. A config is a Kubernetes pod in the case's namespace, a config's namespace being its
// namespace tag, unless it says otherwise.
func (c *Config) Prepare(f *Fixture) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Name == "" {
		c.Name = f.Name("config")
	}
	if c.Type == "" {
		c.Type = "Kubernetes::Pod"
	}
	if c.ConfigClass == "" {
		c.ConfigClass = strings.TrimPrefix(c.Type, "Kubernetes::")
	}
	if c.Config == "" {
		c.Config = `{"kind": "Pod"}`
	}
	if c.Tags == nil {
		c.Tags = map[string]string{}
	}
	if c.Tags["namespace"] == "" {
		c.Tags["namespace"] = f.Namespace
	}
}

// CreateRequest implements Resource.
func (c *Config) CreateRequest() Request {
	return POST("/db/config_items", c)
}

// Path implements Resource.
func (c *Config) Path() string { return "/db/config_items?id=eq." + c.ID }
