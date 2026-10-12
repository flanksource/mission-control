package auth

import (
	"github.com/flanksource/duty/context"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/db"
)

// withPersonAgent sets the agent owned by the authenticated person on the context.
// The agent is taken only from the person's identity, never from the request,
// so an agent's access token always acts as that agent.
func withPersonAgent(ctx context.Context, personID uuid.UUID) (context.Context, error) {
	agent, err := db.FindAgentByPersonID(ctx, personID)
	if err != nil {
		return ctx, ctx.Oops().Wrapf(err, "failed to find agent for person %s", personID)
	}
	if agent != nil {
		ctx = ctx.WithAgent(*agent)
	}
	return ctx, nil
}
