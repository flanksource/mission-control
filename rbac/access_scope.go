package rbac

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// GetAccessScopesForPerson retrieves all AccessScopes for a person (including team memberships)
func GetAccessScopesForPerson(ctx context.Context, personID uuid.UUID) ([]models.AccessScope, error) {
	var scopes []models.AccessScope

	// Get person's direct scopes
	var directScopes []models.AccessScope
	err := ctx.DB().
		Where("person_id = ?", personID).
		Where("deleted_at IS NULL").
		Find(&directScopes).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query person access scopes: %w", err)
	}
	scopes = append(scopes, directScopes...)

	// Get team scopes for teams the person belongs to
	var teamIDs []uuid.UUID
	err = ctx.DB().Table("team_members").
		Select("team_id").
		Where("person_id = ?", personID).
		Pluck("team_id", &teamIDs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query team memberships: %w", err)
	}

	if len(teamIDs) > 0 {
		var teamScopes []models.AccessScope
		err = ctx.DB().
			Where("team_id IN ?", teamIDs).
			Where("deleted_at IS NULL").
			Find(&teamScopes).Error
		if err != nil {
			return nil, fmt.Errorf("failed to query team access scopes: %w", err)
		}
		scopes = append(scopes, teamScopes...)
	}

	return scopes, nil
}

// GetRLSPayloadFromAccessScopes converts AccessScopes into an RLS payload
// Multiple scopes within a single AccessScope are OR'd
// Multiple AccessScope resources are OR'd
func GetRLSPayloadFromAccessScopes(ctx context.Context, accessScopes []models.AccessScope) (*rls.Payload, error) {
	payload := &rls.Payload{}

	for _, accessScope := range accessScopes {
		var scopes []AccessScopeScope
		if err := json.Unmarshal(accessScope.Scopes, &scopes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal scopes: %w", err)
		}

		// Add each scope criteria as a separate filter (OR logic)
		for _, scopeRaw := range scopes {
			agentIDs := make([]string, 0, len(scopeRaw.Agents))
			for _, identifier := range scopeRaw.Agents {
				agent, err := query.FindCachedAgent(ctx, identifier)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve agent %s: %w", identifier, err)
				} else if agent != nil {
					agentIDs = append(agentIDs, agent.ID.String())
				}
			}

			scope := rls.Scope{
				Tags:   scopeRaw.Tags,
				Agents: agentIDs,
				Names:  scopeRaw.Names,
			}

			isWildcardResource := lo.Contains(accessScope.Resources, "*")
			if isWildcardResource || lo.Contains(accessScope.Resources, string(v1.AccessScopeResourceConfig)) {
				payload.Config = append(payload.Config, scope)
			}
			if isWildcardResource || lo.Contains(accessScope.Resources, string(v1.AccessScopeResourceCanary)) {
				payload.Canary = append(payload.Canary, scope)
			}
			if isWildcardResource || lo.Contains(accessScope.Resources, string(v1.AccessScopeResourcePlaybook)) {
				payload.Playbook = append(payload.Playbook, scope)
			}
			if isWildcardResource || lo.Contains(accessScope.Resources, string(v1.AccessScopeResourceComponent)) {
				payload.Component = append(payload.Component, scope)
			}
		}
	}

	return payload, nil
}

// AccessScopeScope mirrors the CRD structure for unmarshaling
type AccessScopeScope struct {
	Tags   map[string]string `json:"tags,omitempty"`
	Agents []string          `json:"agents,omitempty"`
	Names  []string          `json:"names,omitempty"`
}
