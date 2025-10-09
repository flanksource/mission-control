package rbac

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// GetScopeBindingsForPerson retrieves all ScopeBindings for a person (including team memberships)
func GetScopeBindingsForPerson(ctx context.Context, personID uuid.UUID) ([]models.ScopeBinding, error) {
	var person models.Person
	if err := ctx.DB().Where("id = ?", personID).First(&person).Error; err != nil {
		return nil, fmt.Errorf("failed to find person: %w", err)
	}

	var bindings []models.ScopeBinding

	// Get bindings by person email
	var personBindings []models.ScopeBinding
	err := ctx.DB().
		Where("? = ANY(persons)", person.Email).
		Where("deleted_at IS NULL").
		Find(&personBindings).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query person scope bindings: %w", err)
	}
	bindings = append(bindings, personBindings...)

	// Get team memberships
	var teamNames []string
	err = ctx.DB().Table("team_members").
		Select("teams.name").
		Joins("JOIN teams ON teams.id = team_members.team_id").
		Where("team_members.person_id = ?", personID).
		Pluck("name", &teamNames).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query team memberships: %w", err)
	}

	// Get bindings by team
	if len(teamNames) > 0 {
		for _, teamName := range teamNames {
			var teamBindings []models.ScopeBinding
			err = ctx.DB().
				Where("? = ANY(teams)", teamName).
				Where("deleted_at IS NULL").
				Find(&teamBindings).Error
			if err != nil {
				return nil, fmt.Errorf("failed to query team scope bindings: %w", err)
			}
			bindings = append(bindings, teamBindings...)
		}
	}

	return bindings, nil
}

// GetScopesForPerson retrieves all Scopes referenced by a person's ScopeBindings
func GetScopesForPerson(ctx context.Context, personID uuid.UUID) ([]models.Scope, error) {
	bindings, err := GetScopeBindingsForPerson(ctx, personID)
	if err != nil {
		return nil, err
	}

	// Collect all unique scope names grouped by namespace
	type scopeRef struct {
		namespace string
		name      string
	}
	scopeRefs := make(map[scopeRef]bool)

	for _, binding := range bindings {
		for _, scopeName := range binding.Scopes {
			scopeRefs[scopeRef{namespace: binding.Namespace, name: scopeName}] = true
		}
	}

	if len(scopeRefs) == 0 {
		return []models.Scope{}, nil
	}

	// Query all referenced scopes
	var scopes []models.Scope
	for ref := range scopeRefs {
		var s models.Scope
		err := ctx.DB().
			Where("name = ? AND namespace = ?", ref.name, ref.namespace).
			Where("deleted_at IS NULL").
			First(&s).Error
		if err != nil {
			ctx.Logger.Warnf("scope %s/%s referenced but not found: %v", ref.namespace, ref.name, err)
			continue
		}
		scopes = append(scopes, s)
	}

	return scopes, nil
}

// ScopeTargetWithType wraps a target with its resource type
type ScopeTargetWithType struct {
	ResourceType v1.ScopeResourceType
	Selector     v1.ScopeResourceSelector
}

// ExtractTargetsFromScopes extracts all targets from scopes, tagged with their resource type
func ExtractTargetsFromScopes(scopes []models.Scope) ([]ScopeTargetWithType, error) {
	var allTargets []ScopeTargetWithType

	for _, scope := range scopes {
		var targets []v1.ScopeTarget
		if err := json.Unmarshal(scope.Targets, &targets); err != nil {
			return nil, fmt.Errorf("failed to unmarshal targets for scope %s: %w", scope.Name, err)
		}

		for _, target := range targets {
			resourceType, selector := target.GetResourceType()
			if resourceType == "" || selector == nil {
				// Skip invalid targets (multiple or no resource types)
				continue
			}

			allTargets = append(allTargets, ScopeTargetWithType{
				ResourceType: resourceType,
				Selector:     *selector,
			})
		}
	}

	return allTargets, nil
}
