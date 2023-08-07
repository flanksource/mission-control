package agent

import (
	"fmt"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"golang.org/x/crypto/bcrypt"
)

func generateAgent(ctx *api.Context, body api.GenerateAgentRequest) (*api.GeneratedAgent, error) {
	username, password, err := genUsernamePassword()
	if err != nil {
		return nil, fmt.Errorf("failed to generate username and password: %w", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	id, err := db.CreatePerson(ctx, username, string(hashedPassword))
	if err != nil {
		return nil, fmt.Errorf("failed to create a new person: %w", err)
	}

	if _, err := rbac.Enforcer.AddRoleForUser(id.String(), "agent"); err != nil {
		return nil, fmt.Errorf("failed to add 'agent' role to the new person: %w", err)
	}

	if err := db.CreateAgent(ctx, body.Name, &id, body.Properties); err != nil {
		return nil, fmt.Errorf("failed to create a new agent: %w", err)
	}

	return &api.GeneratedAgent{
		ID:          id.String(),
		Username:    username,
		AccessToken: password,
	}, nil
}
