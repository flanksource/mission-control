// Package registry maintains the in-memory map of installed plugins for the
// host process. It is the single source of truth for "what plugins are
// running, what tabs do they expose, and where do I send a request to them".
//
// State is intentionally kept in memory only — Plugin CRDs are the
// persistence layer. On startup the kopper reconciler replays each Plugin
// CRD to populate the registry.
package registry

import (
	"fmt"
	"sync"

	v1 "github.com/flanksource/incident-commander/api/v1"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

// Entry is everything the host needs to route a request to one plugin.
// Manifest is populated by the supervisor once the plugin completes
// RegisterPlugin; it is nil while the plugin is starting up or has crashed
// past the supervisor's restart budget.
type Entry struct {
	Spec     v1.PluginSpec
	Manifest *pluginpb.PluginManifest
	// Supervisor is opaque here; the supervisor package sets it.
	Supervisor any
}

// Registry is the host-side in-memory store of plugins keyed by name.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*Entry
}

// Default is the singleton used by the kopper reconciler and by every echo
// handler that needs to look up a plugin by name.
var Default = New()

// New returns a fresh, empty registry. Tests use this; production uses Default.
func New() *Registry {
	return &Registry{plugins: map[string]*Entry{}}
}

// Upsert replaces the spec for a plugin (called by the kopper reconciler when
// a Plugin CRD is created or updated). Existing manifest/supervisor are
// preserved so an in-flight plugin keeps running across CRD updates that
// don't change the binary.
func (r *Registry) Upsert(name string, spec v1.PluginSpec) *Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[name]
	if !ok {
		e = &Entry{}
		r.plugins[name] = e
	}
	e.Spec = spec
	return e
}

// SetSupervisor attaches a running supervisor to an existing entry. The
// supervisor type is opaque here to avoid an import cycle.
func (r *Registry) SetSupervisor(name string, sup any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not registered", name)
	}
	e.Supervisor = sup
	return nil
}

// SetManifest stores the manifest a plugin returned from RegisterPlugin.
func (r *Registry) SetManifest(name string, m *pluginpb.PluginManifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not registered", name)
	}
	e.Manifest = m
	return nil
}

// Get returns the entry for the given plugin name, or nil if no plugin by
// that name is registered.
func (r *Registry) Get(name string) *Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.plugins[name]; ok {
		return e
	}
	return nil
}

// Remove drops a plugin from the registry. Callers are responsible for
// stopping the supervisor first.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, name)
}

// List returns a snapshot of all registered plugins.
func (r *Registry) List() []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, 0, len(r.plugins))
	for _, e := range r.plugins {
		out = append(out, e)
	}
	return out
}
