// package tunnel is about the infrastructure that allows an agent to
// establish an HTTP tunnel via hashicorp/yamux on which upstream can talk back to the agents.
package tunnel

import (
	"context"
	"net"

	"github.com/google/uuid"
)

// Opens a new yamux session on the agent
func Open(ctx context.Context, agentID uuid.UUID) (net.Conn, error) {
	return defaultManager.Open(ctx, agentID)
}
