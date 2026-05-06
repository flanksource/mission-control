package arthas

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Session is an active Arthas attachment in a pod. The Stop closure must be
// safe to call multiple times; it tears down the in-pod Arthas process (best
// effort) and closes the associated port-forwards.
type Session struct {
	ID            string    `json:"id"`
	Namespace     string    `json:"namespace"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	Pod           string    `json:"pod"`
	Container     string    `json:"container"`
	HTTPLocalPort int       `json:"httpLocalPort"`
	MCPLocalPort  int       `json:"mcpLocalPort"`
	StartedAt     time.Time `json:"startedAt"`

	// JavaVersion is the major version detected via `java -version` in the
	// target container (8, 11, 17, 21, ...). Zero if detection was bypassed.
	JavaVersion int `json:"javaVersion,omitempty"`
	// JDKProvisioned is true when we side-loaded a JDK into the container
	// because the runtime was JRE-only (Java 8 without tools.jar).
	JDKProvisioned bool `json:"jdkProvisioned,omitempty"`
	// SideloadedJavaHome is the path inside the pod where the downloaded JDK
	// lives, when JDKProvisioned is true.
	SideloadedJavaHome string `json:"sideloadedJavaHome,omitempty"`
	// MCPEnabled is true when the arthas mcp-server plugin was successfully
	// started. Upstream arthas 4.1.x doesn't bundle MCP, so this is often
	// false; clients should fall back to arthas' native HTTP /api endpoint.
	MCPEnabled bool `json:"mcpEnabled"`

	stop     func() error
	stopOnce sync.Once
}

// Stop tears down the session. Safe to call multiple times.
func (s *Session) Stop() error {
	var err error
	s.stopOnce.Do(func() {
		if s.stop != nil {
			err = s.stop()
		}
	})
	return err
}

// SessionRegistry holds the set of active sessions for one CLI process.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{sessions: make(map[string]*Session)}
}

// Add stores the session and returns it. The Stop function on the passed-in
// session is installed by the caller (via NewSession helper) before Add.
func (r *SessionRegistry) Add(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
}

func (r *SessionRegistry) Get(id string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

// List returns a snapshot sorted by start time (oldest first).
func (r *SessionRegistry) List() []*Session {
	r.mu.RLock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// Remove stops and forgets the session. Returns false if unknown.
func (r *SessionRegistry) Remove(id string) (bool, error) {
	r.mu.Lock()
	s, ok := r.sessions[id]
	if ok {
		delete(r.sessions, id)
	}
	r.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, s.Stop()
}

// StopAll stops every session; errors are swallowed (shutdown best-effort).
func (r *SessionRegistry) StopAll() {
	r.mu.Lock()
	snapshot := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		snapshot = append(snapshot, s)
	}
	r.sessions = make(map[string]*Session)
	r.mu.Unlock()
	for _, s := range snapshot {
		_ = s.Stop()
	}
}

// NewSession builds a Session with a random ID and installs the Stop closure.
func NewSession(namespace, kind, name, pod, container string, httpPort, mcpPort int, stop func() error) *Session {
	return &Session{
		ID:            newID(),
		Namespace:     namespace,
		Kind:          kind,
		Name:          name,
		Pod:           pod,
		Container:     container,
		HTTPLocalPort: httpPort,
		MCPLocalPort:  mcpPort,
		StartedAt:     time.Now().UTC(),
		stop:          stop,
	}
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("s%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
