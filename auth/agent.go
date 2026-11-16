package auth

import (
	"github.com/flanksource/duty/context"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// withPersonAgent sets the agent owned by the authenticated person on the context.
// The agent is taken only from the person's identity, never from the request,
// so an agent's access token always acts as that agent.
// Only a person of type agent has one: an agent row's person_id is writable, so pointing it at a human or a service
// account must not make them an agent, nor lift their tokens' limits.
func withPersonAgent(ctx context.Context, personID uuid.UUID) (context.Context, error) {
	agent, err := db.FindAgentByPersonID(ctx, personID)
	if err != nil {
		return ctx, ctx.Oops().Wrapf(err, "failed to find agent for person %s", personID)
	} else if agent == nil {
		return ctx, nil
	}

	if isAgent, err := adapter.UnlimitedTokenOwners.Has(ctx.DB(), personID); err != nil {
		return ctx, ctx.Oops().Wrapf(err, "failed to get person %s", personID)
	} else if !isAgent {
		return ctx, nil
	}
	return ctx.WithAgent(*agent), nil
}
