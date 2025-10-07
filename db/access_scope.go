package db

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// PersistAccessScopeFromCRD saves an AccessScope CRD to the database
func PersistAccessScopeFromCRD(ctx context.Context, obj *v1.AccessScope) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return fmt.Errorf("failed to parse UID: %w", err)
	}

	subPersonID, subTeamID, err := resolveAccessScopeSubject(ctx, obj.Spec.Subject)
	if err != nil {
		return fmt.Errorf("failed to resolve subject: %w", err)
	}

	// Marshal scopes to JSON for storage
	scopesJSON, err := json.Marshal(obj.Spec.Scopes)
	if err != nil {
		return fmt.Errorf("failed to marshal scopes: %w", err)
	}

	accessScope := models.AccessScope{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		PersonID:    subPersonID,
		TeamID:      subTeamID,
		Resources:   lo.Map(obj.Spec.Resources, func(r v1.AccessScopeResourceType, _ int) string { return string(r) }),
		Scopes:      types.JSON(scopesJSON),
		Source:      models.SourceCRD,
	}

	return ctx.DB().Save(&accessScope).Error
}

// resolveAccessScopeSubject resolves the subject to person or team ID
// Returns (personID, teamID, error) - exactly one will be non-nil
func resolveAccessScopeSubject(ctx context.Context, subject v1.AccessScopeSubject) (*uuid.UUID, *uuid.UUID, error) {
	if subject.Empty() {
		return nil, nil, fmt.Errorf("subject not specified")
	}

	if subject.Person != "" {
		// Find person by email
		var person models.Person
		err := ctx.DB().Where("email = ?", subject.Person).First(&person).Error
		if err != nil {
			return nil, nil, fmt.Errorf("person with email %s not found: %w", subject.Person, err)
		}
		return &person.ID, nil, nil
	}

	if subject.Team != "" {
		// Find team by name
		var team models.Team
		err := ctx.DB().Where("name = ?", subject.Team).First(&team).Error
		if err != nil {
			return nil, nil, fmt.Errorf("team with name %s not found: %w", subject.Team, err)
		}
		return nil, &team.ID, nil
	}

	return nil, nil, fmt.Errorf("invalid subject: neither person nor team specified")
}

// DeleteAccessScope soft deletes an AccessScope by ID
func DeleteAccessScope(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.AccessScope{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// DeleteStaleAccessScope soft deletes old AccessScope resources with the same name/namespace
func DeleteStaleAccessScope(ctx context.Context, newer *v1.AccessScope) error {
	return ctx.DB().Model(&models.AccessScope{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}
