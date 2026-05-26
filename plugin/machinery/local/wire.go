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

func Set(id uuid.UUID, s *Supervisor) {
	mu.Lock()
	defer mu.Unlock()
	active[id] = s
}

func Remove(id uuid.UUID) {
	mu.Lock()
	defer mu.Unlock()
	delete(active, id)
}
