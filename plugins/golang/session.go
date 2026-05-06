package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Session struct {
	ID             string    `json:"id"`
	Namespace      string    `json:"namespace"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	Pod            string    `json:"pod"`
	Container      string    `json:"container"`
	PID            int       `json:"pid,omitempty"`
	GopsRemote     int       `json:"gopsRemotePort,omitempty"`
	GopsLocal      int       `json:"gopsLocalPort,omitempty"`
	PprofRemote    int       `json:"pprofRemotePort,omitempty"`
	PprofLocal     int       `json:"pprofLocalPort,omitempty"`
	PprofBasePath  string    `json:"pprofBasePath,omitempty"`
	GopsAvailable  bool      `json:"gopsAvailable"`
	PprofAvailable bool      `json:"pprofAvailable"`
	StartedAt      time.Time `json:"startedAt"`
	Diagnostics    []string  `json:"diagnostics,omitempty"`

	stop     func() error
	stopOnce sync.Once
}

func (s *Session) Stop() error {
	var err error
	s.stopOnce.Do(func() {
		if s.stop != nil {
			err = s.stop()
		}
	})
	return err
}

func (s *Session) Snapshot() Session {
	out := *s
	out.stop = nil
	return out
}

type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{sessions: map[string]*Session{}}
}

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

func (r *SessionRegistry) List() []Session {
	r.mu.RLock()
	out := make([]Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s.Snapshot())
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

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

func (r *SessionRegistry) RunningCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

func NewSession(namespace, kind, name, pod, container string, stop func() error) *Session {
	return &Session{
		ID:        newID(),
		Namespace: namespace,
		Kind:      kind,
		Name:      name,
		Pod:       pod,
		Container: container,
		StartedAt: time.Now().UTC(),
		stop:      stop,
	}
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("s%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
