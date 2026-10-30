package db

import (
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm/clause"

	v1 "github.com/flanksource/incident-commander/api/v1"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersistTeamFromCRD saves a Team CRD to the database, with its members.
// A team with the name of a shipped one isn't stored: the shipped one stays.
func PersistTeamFromCRD(ctx context.Context, obj *v1.Team) error {
	name := teamName(obj)
	var count int64
	if err := ctx.DB().Model(&models.Team{}).
		Where("name = ? AND source = ? AND deleted_at IS NULL", name, adapter.SourceShipped).
		Count(&count).Error; err != nil {
		return ctx.Oops().Wrap(err)
	} else if count > 0 {
		return &kopper.NotReadyError{
			Reason:  ReasonShipped,
			Message: fmt.Sprintf("team %s is shipped with Mission Control and can't be overwritten", name),
		}
	}

	if err := PersistTeam(ctx, obj, models.SourceCRD); err != nil {
		return err
	}

	uid, _ := uuid.Parse(string(obj.GetUID()))
	if err := syncTeamMembers(ctx, uid, obj.Spec.Members); err != nil {
		return ctx.Oops().Wrapf(err, "failed to sync team members")
	}

	return nil
}

// PersistTeam saves a team, without its members.
func PersistTeam(ctx context.Context, obj *v1.Team, source string) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to parse UID")
	}

	systemUser, err := GetSystemUser(ctx)
	if err != nil {
		return ctx.Oops().Wrap(err)
	}

	team := models.Team{
		ID:        uid,
		Name:      teamName(obj),
		Icon:      obj.Spec.Icon,
		Source:    source,
		CreatedBy: systemUser.ID,
	}

	if err := ctx.DB().Save(&team).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to save team")
	}
	return nil
}

func teamName(obj *v1.Team) string {
	return lo.CoalesceOrEmpty(obj.Spec.DisplayName, obj.GetName())
}

// syncTeamMembers syncs the team members from the CRD spec
func syncTeamMembers(ctx context.Context, teamID uuid.UUID, members []string) error {
	// Get existing team members
	var existingMembers []dbModels.TeamMember
	if err := ctx.DB().Where("team_id = ? AND source = ?", teamID, models.SourceCRD).Find(&existingMembers).Error; err != nil {
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
			// A membership an external identity provider added becomes the team's own, which the provider never removes
			if err := ctx.DB().Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "team_id"}, {Name: "person_id"}},
				DoUpdates: clause.Assignments(map[string]any{"source": models.SourceCRD}),
				Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "team_members.source LIKE ?", Vars: []any{FederatedTeamSourcePrefix + "%"}}}},
			}).Create(&dbModels.TeamMember{
				TeamID:   teamID,
				PersonID: personID,
				Source:   models.SourceCRD,
			}).Error; err != nil {
				return ctx.Oops().Wrapf(err, "failed to add member %s to team", personID)
			}
		}
	}

	// Remove members not in the new list
	for personID := range existingMemberIDs {
		if _, exists := newMemberIDs[personID]; !exists {
			if err := ctx.DB().Where("team_id = ? AND person_id = ? AND source = ?", teamID, personID, models.SourceCRD).
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
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}

// DeleteStaleTeam soft deletes old Team resources with the same name/namespace
func DeleteStaleTeam(ctx context.Context, newer *v1.Team) error {
	return ctx.DB().Model(&dbModels.Team{}).
		Where("name = ?", newer.Spec.DisplayName).
		Where("id != ?", newer.UID).
		Where("source IS DISTINCT FROM ?", adapter.SourceShipped).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}
