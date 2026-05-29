package agent

import (
	"fmt"
	"time"

	"github.com/flanksource/commons/rand"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

// generateAgent creates a new person and a new agent and associates them.
func generateAgent(ctx context.Context, body api.GenerateAgentRequest) (*api.GeneratedAgent, error) {
	usernameRaw, err := rand.GenerateRandHex(8)
	if err != nil {
		return nil, err
	}
	username := fmt.Sprintf("agent-%s", usernameRaw)

	person, err := db.CreatePerson(ctx, username, fmt.Sprintf("%s@local", username), db.PersonTypeAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new person: %w", err)
	}

	token, _, err := db.CreateAccessToken(ctx, person.ID, "default", lo.ToPtr(time.Hour*24*365), nil, body.AutoRenew)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new access token: %w", err)
	}

	if err := rbac.AddRoleForUser(person.ID.String(), "agent"); err != nil {
		return nil, fmt.Errorf("failed to add 'agent' role to the new person: %w", err)
	}

	if err := db.CreateAgent(ctx, body.Name, &person.ID, body.Properties); err != nil {
		return nil, fmt.Errorf("failed to create a new agent: %w", err)
	}

	return &api.GeneratedAgent{
		ID:          person.ID.String(),
		Username:    username,
		AccessToken: token.PlainText(),
	}, nil
}

func generateToken(ctx context.Context, body api.GenerateTokenRequest) (*api.GeneratedToken, error) {
	agentName := body.AgentName
	agent, err := db.GetAgent(ctx, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agent[%s]: %w", agentName, err)
	}

	token, _, err := db.CreateAccessToken(ctx, lo.FromPtr(agent.PersonID), "default", lo.ToPtr(time.Hour*24*365), nil, body.AutoRenew)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new access token: %w", err)
	}

	return &api.GeneratedToken{
		ID:          lo.FromPtr(agent.PersonID).String(),
		Username:    agentName,
		AccessToken: token.PlainText(),
	}, nil
}
