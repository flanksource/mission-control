// Package sqltrace owns the per-process state for live SQL Server Extended
// Events traces that the plugin's iframe tails via SSE. One Registry is
// created per plugin process and shared across browser tabs / config items.
// Each trace receives its own *gorm.DB at Start time, resolved from the host
// SQL connection for the viewed config item.
package sqltrace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/plugins/sql-server/internal/xetrace"
)

const (
	traceTTL            = 10 * time.Minute
	defaultPollInterval = time.Second
)

// ActiveTrace is the server-side record of one live or recently-stopped
// XE session. mu-guarded fields are read by handlers polling for new events
// while the drain goroutine writes into them.
type ActiveTrace struct {
	ID           string                `json:"id"`
	SessionName  string                `json:"sessionName"`
	Database     string                `json:"database"`
	ConfigItemID string                `json:"configItemId"`
	StartedAt    time.Time             `json:"startedAt"`
	StopAt       time.Time             `json:"stopAt,omitzero"`
	StoppedAt    time.Time             `json:"stoppedAt,omitzero"`
	Options      xetrace.CreateOptions `json:"options"`

	mu       sync.Mutex
	xe       xeSession
	db       *gorm.DB
	events   []xetrace.Event
	running  bool
	stopOnce sync.Once
	cancel   context.CancelFunc
}

// xeSession is the subset of *xetrace.Session that the registry needs.
// Narrowing the surface lets tests substitute an in-memory fake.
type xeSession interface {
	Poll(ctx context.Context) ([]xetrace.Event, error)
	Drop(ctx context.Context) error
}

func (t *ActiveTrace) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// EventsSince returns events whose Key is newer than sinceKey (i.e.
// everything after the first match). An empty sinceKey returns the full
// buffer.
func (t *ActiveTrace) EventsSince(sinceKey string) []xetrace.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sinceKey == "" {
		out := make([]xetrace.Event, len(t.events))
		copy(out, t.events)
		return out
	}
	for i, e := range t.events {
		if e.Key() == sinceKey {
			out := make([]xetrace.Event, len(t.events)-i-1)
			copy(out, t.events[i+1:])
			return out
		}
	}
	out := make([]xetrace.Event, len(t.events))
	copy(out, t.events)
	return out
}

func (t *ActiveTrace) Result() xetrace.TraceResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	events := make([]xetrace.Event, len(t.events))
	copy(events, t.events)
	stopped := t.StoppedAt
	if stopped.IsZero() {
		stopped = time.Now().UTC()
	}
	return xetrace.TraceResult{
		SessionName: t.SessionName,
		Database:    t.Database,
		StartedAt:   t.StartedAt,
		StoppedAt:   stopped,
		Duration:    stopped.Sub(t.StartedAt),
		Events:      events,
	}
}

func (t *ActiveTrace) stop(parent context.Context) error {
	var firstErr error
	t.stopOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
		if t.xe != nil {
			if err := t.xe.Drop(parent); err != nil {
				firstErr = err
			}
		}
		t.mu.Lock()
		t.running = false
		t.StoppedAt = time.Now().UTC()
		t.mu.Unlock()
	})
	return firstErr
}

// Registry holds every active and recently-stopped trace for the plugin
// process. Safe for concurrent use.
type Registry struct {
	mu      sync.Mutex
	traces  map[string]*ActiveTrace
	nowFunc func() time.Time
}

func NewRegistry() *Registry {
	return &Registry{
		traces:  make(map[string]*ActiveTrace),
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

// StartOptions collapses everything a client can pass to Start.
type StartOptions struct {
	xetrace.CreateOptions
	// ConfigItemID is recorded on the trace so List/UI can group by source.
	ConfigItemID string
	// Duration bounds the trace. Zero means run until Stop.
	Duration time.Duration
	// Poll is the ring-buffer poll interval. Zero defaults to 1s.
	Poll time.Duration
}

// Start creates an XE session against db and kicks off the drain goroutine.
// The returned *ActiveTrace is registered before this function returns so
// subsequent List/Get calls are consistent.
func (r *Registry) Start(ctx context.Context, db *gorm.DB, opts StartOptions) (*ActiveTrace, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	if opts.ExcludeSessionID == 0 {
		sid, err := xetrace.CurrentSessionID(ctx, db)
		if err != nil {
			return nil, err
		}
		opts.ExcludeSessionID = sid
	}
	xe, err := xetrace.Create(ctx, db, opts.CreateOptions)
	if err != nil {
		return nil, err
	}

	now := r.nowFunc()
	trace := &ActiveTrace{
		ID:           newID(),
		SessionName:  xe.Name,
		Database:     opts.DatabaseName,
		ConfigItemID: opts.ConfigItemID,
		StartedAt:    now,
		Options:      opts.CreateOptions,
		xe:           xe,
		db:           db,
		running:      true,
	}
	if opts.Duration > 0 {
		trace.StopAt = now.Add(opts.Duration)
	}

	drainCtx, cancel := r.runContext(opts.Duration)
	trace.cancel = cancel

	r.mu.Lock()
	r.traces[trace.ID] = trace
	r.mu.Unlock()

	pollInterval := opts.Poll
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	go r.runDrain(drainCtx, trace, pollInterval)
	return trace, nil
}

func (r *Registry) runContext(duration time.Duration) (context.Context, context.CancelFunc) {
	if duration <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), duration)
}

func (r *Registry) runDrain(ctx context.Context, trace *ActiveTrace, interval time.Duration) {
	defer func() {
		if err := trace.stop(context.Background()); err != nil {
			logger.Warnf("sqltrace: drop session %q: %v", trace.SessionName, err)
		}
	}()

	err := xetrace.Drain(ctx, trace.xe.(*xetrace.Session), interval, func(e xetrace.Event) {
		trace.mu.Lock()
		trace.events = append(trace.events, e)
		trace.mu.Unlock()
	})
	if err != nil {
		logger.Warnf("sqltrace: drain %q: %v", trace.SessionName, err)
	}
}

func (r *Registry) Stop(id string) (*ActiveTrace, error) {
	r.mu.Lock()
	trace, ok := r.traces[id]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("trace %q not found", id)
	}
	if err := trace.stop(context.Background()); err != nil {
		return trace, err
	}
	return trace, nil
}

func (r *Registry) Get(id string) (*ActiveTrace, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.traces[id]
	return t, ok
}

func (r *Registry) List() []*ActiveTrace {
	r.mu.Lock()
	out := make([]*ActiveTrace, 0, len(r.traces))
	for _, t := range r.traces {
		out = append(out, t)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (r *Registry) Delete(id string) (bool, error) {
	r.mu.Lock()
	trace, ok := r.traces[id]
	if ok {
		delete(r.traces, id)
	}
	r.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, trace.stop(context.Background())
}

func (r *Registry) StopAll() {
	r.mu.Lock()
	snapshot := make([]*ActiveTrace, 0, len(r.traces))
	for _, t := range r.traces {
		snapshot = append(snapshot, t)
	}
	r.mu.Unlock()
	for _, t := range snapshot {
		_ = t.stop(context.Background())
	}
}

func (r *Registry) GC() {
	cutoff := r.nowFunc().Add(-traceTTL)
	r.mu.Lock()
	for id, t := range r.traces {
		t.mu.Lock()
		running := t.running
		stopped := t.StoppedAt
		t.mu.Unlock()
		if !running && !stopped.IsZero() && stopped.Before(cutoff) {
			delete(r.traces, id)
		}
	}
	r.mu.Unlock()
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
