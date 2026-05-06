package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

type ProfileRun struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"sessionId"`
	Kind        string     `json:"kind"`
	Source      string     `json:"source,omitempty"`
	Preference  string     `json:"preference,omitempty"`
	State       string     `json:"state"`
	Seconds     int        `json:"seconds,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	ElapsedMS   int64      `json:"elapsedMs"`
	Bytes       int        `json:"bytes,omitempty"`
	Error       string     `json:"error,omitempty"`
	URL         string     `json:"url,omitempty"`

	data   []byte
	cancel context.CancelFunc
	mu     sync.Mutex
}

func NewProfileRun(sessionID, kind, preference string, seconds int) (*ProfileRun, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProfileRun{
		ID:         newRunID(),
		SessionID:  sessionID,
		Kind:       kind,
		Preference: preference,
		State:      "running",
		Seconds:    seconds,
		StartedAt:  time.Now().UTC(),
		cancel:     cancel,
	}, ctx
}

func (r *ProfileRun) MarkDone(data []byte, source string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.CompletedAt = &now
	r.ElapsedMS = now.Sub(r.StartedAt).Milliseconds()
	r.Source = source
	if r.State == "stopped" {
		if err != nil {
			r.Error = err.Error()
		}
		return
	}
	if err != nil {
		r.State = "failed"
		r.Error = err.Error()
		return
	}
	r.State = "completed"
	r.data = data
	r.Bytes = len(data)
	r.URL = fmt.Sprintf("profiles/%s/%s", r.SessionID, r.ID)
}

func (r *ProfileRun) Stop() {
	r.mu.Lock()
	if r.State == "running" {
		r.State = "stopped"
		now := time.Now().UTC()
		r.CompletedAt = &now
		r.ElapsedMS = now.Sub(r.StartedAt).Milliseconds()
	}
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *ProfileRun) Snapshot() ProfileRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := *r
	out.data = nil
	out.cancel = nil
	out.mu = sync.Mutex{}
	if out.State == "running" {
		out.ElapsedMS = time.Since(out.StartedAt).Milliseconds()
	}
	return out
}

func (r *ProfileRun) Data() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.data))
	copy(out, r.data)
	return out
}

type ProfileRegistry struct {
	mu   sync.RWMutex
	runs map[string]*ProfileRun
}

func NewProfileRegistry() *ProfileRegistry {
	return &ProfileRegistry{runs: map[string]*ProfileRun{}}
}

func (r *ProfileRegistry) Add(run *ProfileRun) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[run.ID] = run
}

func (r *ProfileRegistry) Get(id string) (*ProfileRun, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[id]
	return run, ok
}

func (r *ProfileRegistry) List(sessionID string) []ProfileRun {
	r.mu.RLock()
	out := make([]ProfileRun, 0, len(r.runs))
	for _, run := range r.runs {
		if sessionID == "" || run.SessionID == sessionID {
			out = append(out, run.Snapshot())
		}
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (r *ProfileRegistry) RemoveSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, run := range r.runs {
		if run.SessionID == sessionID {
			run.Stop()
			delete(r.runs, id)
		}
	}
}

func newRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("p%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
