// Package registry maintains the in-memory map of installed plugins for the
// host process. It is the single source of truth for "what plugins are
// running, what tabs do they expose, and where do I send a request to them".
//
// State is intentionally kept in memory only — Plugin CRDs are the
// persistence layer. On startup the kopper reconciler replays each Plugin
// CRD to populate the registry.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/google/uuid"
)

var ErrAmbiguousPlugin = errors.New("ambiguous plugin reference")

// Runtime is the host-side handle for a reachable plugin.
type Runtime interface {
	Invoke(context.Context, *api.InvokeRequest) (*api.InvokeResponse, error)
	UIPort() uint32
	Stop()
}

// Entry is everything the host needs to route a request to one plugin.
// Manifest is populated by the supervisor once the plugin completes
// RegisterPlugin; it is nil while the plugin is starting up or has crashed
// past the supervisor's restart budget.
type Entry struct {
	ID            uuid.UUID
	Name          string
	Namespace     string
	Spec          v1.PluginSpec
	Kind          api.Kind
	Manifest      *api.PluginManifest
	Runtime       Runtime
	InstalledPath string

	// An agent id implies this is a proxied plugin.
	// The agent hosts it and mission-control must proxy all operation calls to it.
	AgentID *uuid.UUID
}

// Registry is the host-side in-memory store of plugins.
type Registry struct {
	mu sync.RWMutex

	// plugins is the authoritative store keyed by plugin ID/UID. All runtime
	// routing uses this key after a user-facing reference is resolved.
	plugins map[uuid.UUID]*Entry

	// refs maps exact plugin references to plugin IDs. The reference format is
	// "namespace/name" for namespaced plugins and "/name" for plugins without a
	// namespace. Bare-name lookups are resolved by scanning plugins so duplicate
	// names can be reported as ambiguous instead of stored in this index.
	refs map[string]uuid.UUID
}

// DefaultRegistry is the singleton used by the kopper reconciler and by every echo
// handler that needs to look up a plugin.
var DefaultRegistry = New()

// New returns a fresh, empty registry. Tests use this; production uses Default.
func New() *Registry {
	return &Registry{
		plugins: map[uuid.UUID]*Entry{},
		refs:    map[string]uuid.UUID{},
	}
}

// Upsert replaces the spec for a local plugin (called by the kopper reconciler
// when a Plugin CRD is created or updated). Existing manifest/supervisor are
// preserved so an in-flight plugin keeps running across CRD updates that
// don't change the binary.
func (r *Registry) Upsert(id uuid.UUID, namespace, name string, spec v1.PluginSpec) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.upsertLocked(id, namespace, name, spec, api.PluginKindLocal, nil, nil)
}

// UpsertProxied registers a plugin runtime reported by an authenticated agent.
func (r *Registry) UpsertProxied(id uuid.UUID, namespace, name string, spec v1.PluginSpec, manifest *api.PluginManifest, agentID uuid.UUID) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if agentID == uuid.Nil {
		return nil, fmt.Errorf("agent id is required")
	}
	return r.upsertLocked(id, namespace, name, spec, api.PluginKindProxied, &agentID, manifest)
}

func (r *Registry) upsertLocked(id uuid.UUID, namespace, name string, spec v1.PluginSpec, kind api.Kind, agentID *uuid.UUID, manifest *api.PluginManifest) (*Entry, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("plugin id is required")
	}
	name = strings.TrimSpace(name)
	namespace = strings.TrimSpace(namespace)

	ref := namespacedName(namespace, name)
	if existingID := r.refs[ref]; existingID != uuid.Nil && existingID != id {
		return nil, fmt.Errorf("plugin %s is already registered with id %s", ref, existingID)
	}

	if existing := r.plugins[id]; existing != nil {
		r.removeIndexes(existing)
	}

	e := r.plugins[id]
	if e == nil {
		e = &Entry{ID: id}
		r.plugins[id] = e
	}
	e.ID = id
	e.Name = name
	e.Namespace = namespace
	e.Spec = spec
	e.Kind = kind
	e.AgentID = agentID
	if manifest != nil {
		e.Manifest = manifest
	}
	r.addIndexes(e)
	return e, nil
}

// SetManifest stores the manifest a plugin returned from RegisterPlugin.
func (r *Registry) SetManifest(id uuid.UUID, m *api.PluginManifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %s not registered", id)
	}
	e.Manifest = m
	return nil
}

// Get returns the entry for the given plugin id, or nil if no plugin by that
// id is registered.
func (r *Registry) Get(id uuid.UUID) *Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.plugins[id]; ok {
		return snapshotEntry(e)
	}
	return nil
}

// Resolve accepts a plugin id, namespace/name, or unqualified name. Name-based
// references are resolved to ids internally; unqualified names must be unique.
func (r *Registry) Resolve(ref string) (*Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}

	if id, err := uuid.Parse(ref); err == nil {
		if e, ok := r.plugins[id]; ok {
			return snapshotEntry(e), nil
		}
		return nil, nil
	}

	if strings.Contains(ref, "/") {
		if id := r.refs[ref]; id != uuid.Nil {
			return snapshotEntry(r.plugins[id]), nil
		}
		return nil, nil
	}

	matches := make([]*Entry, 0, 1)
	for _, e := range r.plugins {
		if e.Name == ref {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		refs := make([]string, 0, len(matches))
		for _, e := range matches {
			refs = append(refs, displayRef(e))
		}
		sort.Strings(refs)
		return nil, fmt.Errorf("%w %q; use plugin id or namespace/name, matches: %s", ErrAmbiguousPlugin, ref, strings.Join(refs, ", "))
	}
	return snapshotEntry(matches[0]), nil
}

// SetRuntime stores the running-process handle for a plugin.
func (r *Registry) SetRuntime(id uuid.UUID, runtime Runtime) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %s not registered", id)
	}
	e.Runtime = runtime
	return nil
}

// SetInstalledPath stores the executable path installed for a plugin.
func (r *Registry) SetInstalledPath(id uuid.UUID, path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %s not registered", id)
	}
	e.InstalledPath = path
	return nil
}

// SetRuntimeIfAbsent stores the runtime only when no runtime is already active.
func (r *Registry) SetRuntimeIfAbsent(id uuid.UUID, runtime Runtime) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[id]
	if !ok {
		return false, fmt.Errorf("plugin %s not registered", id)
	}
	if e.Runtime != nil {
		return false, nil
	}
	e.Runtime = runtime
	return true, nil
}

// PopRuntime removes and returns the running-process handle for a plugin.
func (r *Registry) PopRuntime(id uuid.UUID) Runtime {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[id]
	if !ok {
		return nil
	}
	runtime := e.Runtime
	e.Runtime = nil
	return runtime
}

// Remove drops a plugin from the registry. Callers are responsible for
// stopping the supervisor first.
func (r *Registry) Remove(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.plugins[id]; ok {
		r.removeIndexes(e)
	}
	delete(r.plugins, id)
}

// List returns a snapshot of all registered plugins.
func (r *Registry) List() []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, 0, len(r.plugins))
	for _, e := range r.plugins {
		out = append(out, snapshotEntry(e))
	}
	return out
}

func (r *Registry) addIndexes(e *Entry) {
	if e.Name == "" {
		return
	}
	r.refs[namespacedName(e.Namespace, e.Name)] = e.ID
}

func (r *Registry) removeIndexes(e *Entry) {
	if e.Name == "" {
		return
	}
	ref := namespacedName(e.Namespace, e.Name)
	if r.refs[ref] == e.ID {
		delete(r.refs, ref)
	}
}

func namespacedName(namespace, name string) string {
	return namespace + "/" + name
}

func displayRef(e *Entry) string {
	ref := namespacedName(e.Namespace, e.Name)
	if ref == "" {
		return e.ID.String()
	}
	return ref
}

func snapshotEntry(e *Entry) *Entry {
	if e == nil {
		return nil
	}
	copy := *e
	return &copy
}
