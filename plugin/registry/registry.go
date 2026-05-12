// Package registry maintains the in-memory map of installed plugins for the
// host process. It is the single source of truth for "what plugins are
// running, what tabs do they expose, and where do I send a request to them".
//
// State is intentionally kept in memory only — Plugin CRDs are the
// persistence layer. On startup the kopper reconciler replays each Plugin
// CRD to populate the registry.
package registry

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	v1 "github.com/flanksource/incident-commander/api/v1"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

var ErrAmbiguousPlugin = errors.New("ambiguous plugin reference")

// Entry is everything the host needs to route a request to one plugin.
// Manifest is populated by the supervisor once the plugin completes
// RegisterPlugin; it is nil while the plugin is starting up or has crashed
// past the supervisor's restart budget.
type Entry struct {
	ID        string
	Name      string
	Namespace string
	Spec      v1.PluginSpec
	Manifest  *pluginpb.PluginManifest
	// Supervisor is opaque here; the supervisor package sets it.
	Supervisor any
}

// Registry is the host-side in-memory store of plugins keyed by id. Name and
// namespace/name indexes exist only to resolve user-facing references to ids.
type Registry struct {
	mu sync.RWMutex

	plugins     map[string]*Entry
	nameIndex   map[string]map[string]struct{}
	nsNameIndex map[string]string
}

// Default is the singleton used by the kopper reconciler and by every echo
// handler that needs to look up a plugin.
var Default = New()

// New returns a fresh, empty registry. Tests use this; production uses Default.
func New() *Registry {
	return &Registry{
		plugins:     map[string]*Entry{},
		nameIndex:   map[string]map[string]struct{}{},
		nsNameIndex: map[string]string{},
	}
}

// Upsert replaces the spec for a plugin (called by the kopper reconciler when
// a Plugin CRD is created or updated). Existing manifest/supervisor are
// preserved so an in-flight plugin keeps running across CRD updates that
// don't change the binary.
func (r *Registry) Upsert(id, namespace, name string, spec v1.PluginSpec) *Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	namespace = strings.TrimSpace(namespace)
	if id == "" {
		id = namespacedName(namespace, name)
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
	r.addIndexes(e)
	return e
}

// SetSupervisor attaches a running supervisor to an existing entry. The
// supervisor type is opaque here to avoid an import cycle.
func (r *Registry) SetSupervisor(id string, sup any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %q not registered", id)
	}
	e.Supervisor = sup
	return nil
}

// SetManifest stores the manifest a plugin returned from RegisterPlugin.
func (r *Registry) SetManifest(id string, m *pluginpb.PluginManifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %q not registered", id)
	}
	e.Manifest = m
	return nil
}

// Get returns the entry for the given plugin id, or nil if no plugin by that
// id is registered.
func (r *Registry) Get(id string) *Entry {
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

	if e, ok := r.plugins[ref]; ok {
		return snapshotEntry(e), nil
	}

	if strings.Contains(ref, "/") {
		if id := r.nsNameIndex[ref]; id != "" {
			return snapshotEntry(r.plugins[id]), nil
		}
		return nil, nil
	}

	ids := r.nameIndex[ref]
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 1 {
		matches := make([]string, 0, len(ids))
		for id := range ids {
			if e := r.plugins[id]; e != nil {
				matches = append(matches, displayRef(e))
			}
		}
		sort.Strings(matches)
		return nil, fmt.Errorf("%w %q; use plugin id or namespace/name, matches: %s", ErrAmbiguousPlugin, ref, strings.Join(matches, ", "))
	}
	for id := range ids {
		return snapshotEntry(r.plugins[id]), nil
	}
	return nil, nil
}

// Remove drops a plugin from the registry. Callers are responsible for
// stopping the supervisor first.
func (r *Registry) Remove(id string) {
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
	ids := r.nameIndex[e.Name]
	if ids == nil {
		ids = map[string]struct{}{}
		r.nameIndex[e.Name] = ids
	}
	ids[e.ID] = struct{}{}
	if e.Namespace != "" {
		r.nsNameIndex[namespacedName(e.Namespace, e.Name)] = e.ID
	}
}

func (r *Registry) removeIndexes(e *Entry) {
	if e.Name == "" {
		return
	}
	if ids := r.nameIndex[e.Name]; ids != nil {
		delete(ids, e.ID)
		if len(ids) == 0 {
			delete(r.nameIndex, e.Name)
		}
	}
	if e.Namespace != "" {
		key := namespacedName(e.Namespace, e.Name)
		if r.nsNameIndex[key] == e.ID {
			delete(r.nsNameIndex, key)
		}
	}
}

func namespacedName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

func displayRef(e *Entry) string {
	ref := namespacedName(e.Namespace, e.Name)
	if ref == "" {
		return e.ID
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
