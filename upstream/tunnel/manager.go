package tunnel

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
)

var (
	ErrSessionNotFound = errors.New("a yamux session was not found for the agent")
	ErrSessionClosed   = errors.New("yamux session is already closed")
)

type manager struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*yamux.Session
}

var defaultManager = newManager()

func newManager() *manager {
	return &manager{sessions: map[uuid.UUID]*yamux.Session{}}
}

func (t *manager) Register(agentID uuid.UUID, session *yamux.Session) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if s, ok := t.sessions[agentID]; ok {
		s.Close()
	}
	t.sessions[agentID] = session
}

// DeleteIfSame deletes the agent yamux session.
// Returns true if the session was actually deleted.
func (t *manager) DeleteIfSame(agentID uuid.UUID, session *yamux.Session) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.sessions[agentID]
	if !ok {
		return false
	}

	if s == session {
		delete(t.sessions, agentID)
		return true
	}

	return false
}

func (t *manager) Open(ctx context.Context, agentID uuid.UUID) (net.Conn, error) {
	t.mu.Lock()
	session, ok := t.sessions[agentID]
	if !ok {
		t.mu.Unlock()
		return nil, ErrSessionNotFound
	}

	if session.IsClosed() {
		t.mu.Unlock()
		return nil, ErrSessionClosed
	}
	t.mu.Unlock()

	ch := make(chan struct {
		conn net.Conn
		err  error
	})

	go func() {
		conn, err := session.Open()
		res := struct {
			conn net.Conn
			err  error
		}{conn, err}

		select {
		case ch <- res:
		case <-ctx.Done():
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.conn, res.err
	}
}
