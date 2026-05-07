package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ProfileViewer wraps a `go tool pprof -http` process serving a single captured run.
type ProfileViewer struct {
	RunID     string
	SessionID string
	Addr      string

	cmd      *exec.Cmd
	tmpFile  string
	lastUsed time.Time
}

// ProfileViewerRegistry lazily spawns and reaps pprof viewers for completed runs.
type ProfileViewerRegistry struct {
	mu      sync.Mutex
	viewers map[string]*ProfileViewer
	idleTTL time.Duration
}

func NewProfileViewerRegistry() *ProfileViewerRegistry {
	return &ProfileViewerRegistry{
		viewers: map[string]*ProfileViewer{},
		idleTTL: 10 * time.Minute,
	}
}

// Get returns the viewer address for run, spawning a viewer if one is not running.
// Trace runs are unsupported because `go tool pprof` does not parse trace files.
func (r *ProfileViewerRegistry) Get(ctx context.Context, run *ProfileRun) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run is nil")
	}
	snap := run.Snapshot()
	if snap.Kind == "trace" {
		return "", fmt.Errorf("trace profiles cannot be rendered with go tool pprof")
	}
	if snap.State != "completed" {
		return "", fmt.Errorf("profile run is not completed")
	}

	r.mu.Lock()
	if existing, ok := r.viewers[snap.ID]; ok {
		existing.lastUsed = time.Now()
		addr := existing.Addr
		r.mu.Unlock()
		return addr, nil
	}
	r.mu.Unlock()

	viewer, err := spawnProfileViewer(ctx, run)
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.viewers[snap.ID]; ok {
		// Lost a race; shut down the duplicate viewer we just spawned.
		viewer.shutdown()
		existing.lastUsed = time.Now()
		return existing.Addr, nil
	}
	r.viewers[snap.ID] = viewer
	return viewer.Addr, nil
}

// RemoveSession shuts down all viewers belonging to a session.
func (r *ProfileViewerRegistry) RemoveSession(sessionID string) {
	r.mu.Lock()
	doomed := make([]*ProfileViewer, 0)
	for id, v := range r.viewers {
		if v.SessionID == sessionID {
			doomed = append(doomed, v)
			delete(r.viewers, id)
		}
	}
	r.mu.Unlock()
	for _, v := range doomed {
		v.shutdown()
	}
}

// RemoveRun shuts down a specific viewer.
func (r *ProfileViewerRegistry) RemoveRun(runID string) {
	r.mu.Lock()
	v, ok := r.viewers[runID]
	if ok {
		delete(r.viewers, runID)
	}
	r.mu.Unlock()
	if ok {
		v.shutdown()
	}
}

// ReapIdle terminates viewers that have been idle longer than idleTTL.
func (r *ProfileViewerRegistry) ReapIdle(now time.Time) {
	r.mu.Lock()
	doomed := make([]*ProfileViewer, 0)
	for id, v := range r.viewers {
		if now.Sub(v.lastUsed) > r.idleTTL {
			doomed = append(doomed, v)
			delete(r.viewers, id)
		}
	}
	r.mu.Unlock()
	for _, v := range doomed {
		v.shutdown()
	}
}

// StartReaper runs ReapIdle on a ticker until ctx is cancelled.
func (r *ProfileViewerRegistry) StartReaper(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				r.shutdownAll()
				return
			case now := <-t.C:
				r.ReapIdle(now)
			}
		}
	}()
}

func (r *ProfileViewerRegistry) shutdownAll() {
	r.mu.Lock()
	doomed := make([]*ProfileViewer, 0, len(r.viewers))
	for id, v := range r.viewers {
		doomed = append(doomed, v)
		delete(r.viewers, id)
	}
	r.mu.Unlock()
	for _, v := range doomed {
		v.shutdown()
	}
}

func (v *ProfileViewer) shutdown() {
	if v.cmd != nil && v.cmd.Process != nil {
		// Kill the whole process group so any children spawned by `go tool
		// pprof` (e.g. dot, the bundled web UI helper) exit too.
		_ = syscall.Kill(-v.cmd.Process.Pid, syscall.SIGKILL)
		_ = v.cmd.Process.Kill()
		_, _ = v.cmd.Process.Wait()
	}
	if v.tmpFile != "" {
		_ = os.Remove(v.tmpFile)
	}
}

func spawnProfileViewer(ctx context.Context, run *ProfileRun) (*ProfileViewer, error) {
	snap := run.Snapshot()
	data := run.Data()
	if len(data) == 0 {
		return nil, fmt.Errorf("profile run has no data")
	}

	dir := filepath.Join(os.TempDir(), "golang-plugin-profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create profile dir: %w", err)
	}
	tmpPath := filepath.Join(dir, snap.ID+"."+profileExtension(snap.Kind))
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("write profile: %w", err)
	}

	// `go tool pprof -http=127.0.0.1:0` echoes the literal "127.0.0.1:0" to
	// stderr instead of the resolved port, so we pre-allocate an ephemeral
	// port ourselves. There is a small race between closing this listener
	// and pprof binding the same port; in practice it has been reliable.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("reserve port: %w", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	cmd := exec.Command("go", "tool", "pprof", "-http="+addr, "-no_browser", tmpPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("start go tool pprof: %w", err)
	}

	viewer := &ProfileViewer{
		RunID:     snap.ID,
		SessionID: snap.SessionID,
		Addr:      addr,
		cmd:       cmd,
		tmpFile:   tmpPath,
		lastUsed:  time.Now(),
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := waitForListener(waitCtx, viewer.Addr); err != nil {
		viewer.shutdown()
		return nil, err
	}
	return viewer, nil
}

func waitForListener(ctx context.Context, addr string) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(15 * time.Second)
	}
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("pprof viewer at %s did not become reachable", addr)
		}
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/", nil)
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
