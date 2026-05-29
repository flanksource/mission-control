package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/flanksource/duty/upstream"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

// A request an agent makes to register its plugin to the upstream
type PluginRegisterRequest struct {
	ID        uuid.UUID       `json:"id"`
	Namespace string          `json:"namespace,omitempty"`
	Name      string          `json:"name"`
	Spec      v1.PluginSpec   `json:"spec"`
	Manifest  *PluginManifest `json:"manifest,omitempty"`
}

// RegisterWithUpstream reports a local plugin manifest to upstream so upstream
// can register it as a proxied plugin owned by this agent.
func RegisterWithUpstream(ctx context.Context, config upstream.UpstreamConfig, pluginID uuid.UUID) error {
	if !config.Valid() {
		return nil
	}

	entry := DefaultRegistry.Get(pluginID)
	if entry == nil {
		return fmt.Errorf("plugin %s not found in registry", pluginID)
	}
	if entry.Manifest == nil {
		return fmt.Errorf("plugin %s has no manifest", pluginID)
	}
	if entry.Kind == PluginKindProxied {
		return fmt.Errorf("cannot proxy an already proxied plugin. An agent can only proxy plugins that it runs locally")
	}

	req := PluginRegisterRequest{
		ID:        entry.ID,
		Namespace: entry.Namespace,
		Name:      entry.Name,
		Spec:      entry.Spec,
		Manifest:  entry.Manifest,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal plugin register request: %w", err)
	}

	c := upstream.NewUpstreamClient(config)
	resp, err := c.Client.R(ctx).
		Header("Content-Type", "application/json").
		QueryParam(upstream.AgentNameQueryParam, config.AgentName).
		Post("/plugin/register", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("register plugin with upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("register plugin with upstream returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return nil
}
