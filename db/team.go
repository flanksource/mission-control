package db

import (
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
	dbModels "github.com/flanksource/incident-commander/db/models"
)

// PersistTeamFromCRD saves a Team CRD to the database
func PersistTeamFromCRD(ctx context.Context, obj *v1.Team) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to parse UID")
	}

	// Use displayName as the team name if provided, otherwise use metadata.name
	name := obj.Spec.DisplayName
	if name == "" {
		name = obj.GetName()
	}

	var icon *string
	if obj.Spec.Icon != "" {
		icon = &obj.Spec.Icon
	}

	team := dbModels.Team{
		ID:        uid,
		Name:      name,
		Icon:      icon,
		Source:    ptr(models.SourceCRD),
		CreatedBy: SystemUser.ID,
	}

	if err := ctx.DB().Save(&team).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to save team")
	}

	// Sync team members
	if err := syncTeamMembers(ctx, uid, obj.Spec.Members); err != nil {
		return ctx.Oops().Wrapf(err, "failed to sync team members")
	}

	return nil
}

// syncTeamMembers syncs the team members from the CRD spec
func syncTeamMembers(ctx context.Context, teamID uuid.UUID, members []string) error {
	// Get existing team members
	var existingMembers []dbModels.TeamMember
	if err := ctx.DB().Where("team_id = ?", teamID).Find(&existingMembers).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to get existing team members")
	}

	existingMemberIDs := make(map[uuid.UUID]struct{})
	for _, m := range existingMembers {
		existingMemberIDs[m.PersonID] = struct{}{}
	}

	// Resolve member identifiers to person IDs
	newMemberIDs := make(map[uuid.UUID]struct{})
	for _, member := range members {
		personID, err := resolvePersonID(ctx, member)
		if err != nil {
			ctx.Warnf("failed to resolve member %q: %v", member, err)
			continue
		}
		newMemberIDs[personID] = struct{}{}
	}

	// Add new members
	for personID := range newMemberIDs {
		if _, exists := existingMemberIDs[personID]; !exists {
			if err := ctx.DB().Create(&dbModels.TeamMember{
				TeamID:   teamID,
				PersonID: personID,
			}).Error; err != nil {
				return ctx.Oops().Wrapf(err, "failed to add member %s to team", personID)
			}
		}
	}

	// Remove members not in the new list
	for personID := range existingMemberIDs {
		if _, exists := newMemberIDs[personID]; !exists {
			if err := ctx.DB().Where("team_id = ? AND person_id = ?", teamID, personID).
				Delete(&dbModels.TeamMember{}).Error; err != nil {
				return ctx.Oops().Wrapf(err, "failed to remove member %s from team", personID)
			}
		}
	}

	return nil
}

// resolvePersonID resolves a person identifier (UUID or email) to a person ID
func resolvePersonID(ctx context.Context, identifier string) (uuid.UUID, error) {
	// Check if it's already a UUID
	if id, err := uuid.Parse(identifier); err == nil {
		return id, nil
	}

	// Try to find by email
	var person models.Person
	if err := ctx.DB().Where("email = ?", identifier).First(&person).Error; err != nil {
		return uuid.Nil, ctx.Oops().Wrapf(err, "person with email %q not found", identifier)
	}

	return person.ID, nil
}

// DeleteTeam soft deletes a Team by ID
func DeleteTeam(ctx context.Context, id string) error {
	return ctx.DB().Model(&dbModels.Team{}).
		Where("id = ?", id).
		Update("deleted_at", duty.Now()).Error
}

// DeleteStaleTeam soft deletes old Team resources with the same name/namespace
func DeleteStaleTeam(ctx context.Context, newer *v1.Team) error {
	return ctx.DB().Model(&dbModels.Team{}).
		Where("name = ?", newer.Spec.DisplayName).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}

func ptr[T any](v T) *T {
	return &v
}
