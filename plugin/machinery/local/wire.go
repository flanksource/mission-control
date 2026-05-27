package local

import (
	"sync"

	"github.com/google/uuid"
)

// active holds running supervisors so the kopper reconciler can stop them
// on CRD delete.
var (
	mu     sync.Mutex
	active = map[uuid.UUID]*Supervisor{}
)

// LookupSupervisor returns the running supervisor for a plugin id, or nil if
// the plugin is not running. Used by the echo handlers and CLI.
func LookupSupervisor(id uuid.UUID) *Supervisor {
	mu.Lock()
	defer mu.Unlock()
	return active[id]
}

// SetIfAbsent stores the supervisor only when no supervisor is already active.
func SetIfAbsent(id uuid.UUID, s *Supervisor) bool {
	mu.Lock()
	defer mu.Unlock()
	if active[id] != nil {
		return false
	}
	active[id] = s
	return true
}

// RollbackSupervisor removes a supervisor that failed to start, without
// deleting a newer supervisor registered for the same plugin.
func RollbackSupervisor(id uuid.UUID, failed *Supervisor) {
	mu.Lock()
	defer mu.Unlock()
	if active[id] == failed {
		delete(active, id)
	}
}

// PopSupervisor removes and returns the active supervisor for id.
func PopSupervisor(id uuid.UUID) *Supervisor {
	mu.Lock()
	defer mu.Unlock()
	sup := active[id]
	delete(active, id)
	return sup
}
