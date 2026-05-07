package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type TraceTarget struct {
	Namespace string            `json:"namespace"`
	Kind      string            `json:"kind,omitempty"`
	Name      string            `json:"name,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	Container string            `json:"container,omitempty"`
	Node      string            `json:"node,omitempty"`
	Selector  map[string]string `json:"selector,omitempty"`
}

type TraceEvent struct {
	SessionID string         `json:"sessionId"`
	Sequence  int64          `json:"sequence"`
	Time      time.Time      `json:"time"`
	Node      string         `json:"node,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Raw       string         `json:"raw,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type TraceSession struct {
	ID           string            `json:"id"`
	GadgetID     string            `json:"gadgetId"`
	GadgetName   string            `json:"gadgetName"`
	GadgetKind   string            `json:"gadgetKind"`
	GadgetWidget string            `json:"gadgetWidget"`
	GadgetImage  string            `json:"gadgetImage"`
	GadgetTag    string            `json:"gadgetTag"`
	DocsURL      string            `json:"docsUrl,omitempty"`
	Target       TraceTarget       `json:"target"`
	Params       map[string]string `json:"params,omitempty"`
	Diagnostics  TraceDiagnostics  `json:"diagnostics"`
	State        string            `json:"state"`
	StartedAt    time.Time         `json:"startedAt"`
	StoppedAt    *time.Time        `json:"stoppedAt,omitempty"`
	Error        string            `json:"error,omitempty"`
	EventCount   int64             `json:"eventCount"`

	cancel context.CancelFunc
	events chan TraceEvent
	mu     sync.Mutex
	buffer []TraceEvent
	max    int
	seq    int64
}

type TraceDiagnostics struct {
	Runtime         string            `json:"runtime"`
	Connection      string            `json:"connection"`
	GadgetWidget    string            `json:"gadgetWidget,omitempty"`
	GadgetImage     string            `json:"gadgetImage"`
	GadgetTag       string            `json:"gadgetTag"`
	GadgetDocsURL   string            `json:"gadgetDocsUrl,omitempty"`
	DurationSec     int               `json:"durationSec"`
	MaxEvents       int               `json:"maxEvents"`
	MaxSessions     int               `json:"maxSessions"`
	ResolvedPods    []RunningPod      `json:"resolvedPods,omitempty"`
	ResolvedTarget  TraceTarget       `json:"resolvedTarget"`
	RuntimeParams   map[string]string `json:"runtimeParams,omitempty"`
	UserOptions     map[string]any    `json:"userOptions,omitempty"`
	StartedByUserID string            `json:"startedByUserId,omitempty"`
	StartedByEmail  string            `json:"startedByEmail,omitempty"`
}

func newTraceSession(gadget GadgetSpec, target TraceTarget, params map[string]string, diagnostics TraceDiagnostics, maxEvents int) (*TraceSession, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	if maxEvents <= 0 {
		maxEvents = 10000
	}
	diagnostics.GadgetImage = gadget.Image
	diagnostics.GadgetWidget = gadget.Widget
	diagnostics.GadgetTag = tagFromImage(gadget.Image)
	diagnostics.GadgetDocsURL = gadget.DocsURL
	diagnostics.ResolvedTarget = target
	diagnostics.RuntimeParams = params
	diagnostics.MaxEvents = maxEvents
	return &TraceSession{
		ID:           newID(),
		GadgetID:     gadget.ID,
		GadgetName:   gadget.Name,
		GadgetKind:   gadget.Kind,
		GadgetWidget: gadget.Widget,
		GadgetImage:  gadget.Image,
		GadgetTag:    diagnostics.GadgetTag,
		DocsURL:      gadget.DocsURL,
		Target:       target,
		Params:       params,
		Diagnostics:  diagnostics,
		State:        "starting",
		StartedAt:    time.Now(),
		cancel:       cancel,
		events:       make(chan TraceEvent, 256),
		max:          maxEvents,
	}, ctx
}

func (s *TraceSession) AddEvent(event TraceEvent) {
	s.mu.Lock()
	s.seq++
	event.Sequence = s.seq
	event.SessionID = s.ID
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	s.EventCount = s.seq
	s.buffer = append(s.buffer, event)
	if len(s.buffer) > s.max {
		copy(s.buffer, s.buffer[len(s.buffer)-s.max:])
		s.buffer = s.buffer[:s.max]
	}
	s.mu.Unlock()

	select {
	case s.events <- event:
	default:
	}
}

func (s *TraceSession) Snapshot() TraceSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := *s
	out.cancel = nil
	out.events = nil
	out.buffer = nil
	return out
}

func (s *TraceSession) Events() []TraceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TraceEvent, len(s.buffer))
	copy(out, s.buffer)
	return out
}

func (s *TraceSession) MarkRunning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = "running"
}

func (s *TraceSession) MarkDone(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.StoppedAt = &now
	if err != nil {
		s.State = "failed"
		s.Error = err.Error()
	} else if s.State != "stopped" {
		s.State = "completed"
	}
	close(s.events)
}

func (s *TraceSession) Stop() {
	s.mu.Lock()
	s.State = "stopped"
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

type SessionRegistry struct {
	mu        sync.Mutex
	maxEvents int
	sessions  map[string]*TraceSession
}

func NewSessionRegistry(maxEvents int) *SessionRegistry {
	return &SessionRegistry{maxEvents: maxEvents, sessions: map[string]*TraceSession{}}
}

func (r *SessionRegistry) SetMaxEvents(max int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if max > 0 {
		r.maxEvents = max
	}
}

func (r *SessionRegistry) Add(s *TraceSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
}

func (r *SessionRegistry) Get(id string) (*TraceSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}

func (r *SessionRegistry) List() []TraceSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TraceSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s.Snapshot())
	}
	return out
}

func (r *SessionRegistry) RunningCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.sessions {
		state := s.Snapshot().State
		if state == "starting" || state == "running" {
			n++
		}
	}
	return n
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func tagFromImage(image string) string {
	for i := len(image) - 1; i >= 0; i-- {
		if image[i] == ':' {
			return image[i+1:]
		}
		if image[i] == '/' {
			break
		}
	}
	return ""
}
