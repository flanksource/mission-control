package db

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ctx.Warnf("failed to resolve member %q: %v", member, err)
			continue
		} else if err != nil {
			return err
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

// resolvePersonID resolves a person identifier (UUID or email) to the ID of a person who can be a team member
func resolvePersonID(ctx context.Context, identifier string) (uuid.UUID, error) {
	query := ctx.DB().Model(&models.Person{}).Scopes(adapter.TeamMembers.Scope)
	if id, err := uuid.Parse(identifier); err == nil {
		query = query.Where("id = ?", id)
	} else {
		query = query.Where("email = ?", identifier)
	}

	var person models.Person
	if err := query.First(&person).Error; err != nil {
		return uuid.Nil, ctx.Oops().Wrapf(err, "person %q not found", identifier)
	}

	return person.ID, nil
}

// SourceTeamMapper is the source of the team memberships a team mapper adds.
// A team mapper only ever removes memberships with this source.
const SourceTeamMapper = "team-mapper"

// AddTeamMembers adds the people to the team, all or none. A person who can't be a team member, or is one of the
// team already, fails it as not found.
func AddTeamMembers(ctx context.Context, teamID uuid.UUID, personIDs []uuid.UUID) error {
	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		var teams int64
		if err := txCtx.DB().Model(&models.Team{}).Where("id = ? AND deleted_at IS NULL", teamID).Count(&teams).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to get team %s", teamID)
		} else if teams == 0 {
			return api.Errorf(api.ENOTFOUND, "team %s not found", teamID)
		}

		for _, personID := range lo.Uniq(personIDs) {
			var people int64
			if err := txCtx.DB().Model(&models.Person{}).Where("id = ? AND deleted_at IS NULL", personID).
				Scopes(adapter.TeamMembers.Scope).Count(&people).Error; err != nil {
				return ctx.Oops().Wrapf(err, "failed to get person %s", personID)
			} else if people == 0 {
				return api.Errorf(api.ENOTFOUND, "person %s not found", personID)
			}

			result := txCtx.DB().Exec(`INSERT INTO team_members (team_id, person_id, source) VALUES (?, ?, ?)
				ON CONFLICT (team_id, person_id) DO NOTHING`, teamID, personID, models.SourceUI)
			if result.Error != nil {
				return ctx.Oops().Wrapf(result.Error, "failed to add person %s to team %s", personID, teamID)
			} else if result.RowsAffected == 0 {
				return api.Errorf(api.ENOTFOUND, "person %s is already a member of team %s", personID, teamID)
			}
		}
		return nil
	})
}

// RemoveTeamMember removes a direct member of the team. A membership a team mapper or an external identity provider
// added is left to them, and fails it as not found.
func RemoveTeamMember(ctx context.Context, teamID, personID uuid.UUID) error {
	result := ctx.DB().Where("team_id = ? AND person_id = ?", teamID, personID).
		Where("source <> ? AND source NOT LIKE ?", SourceTeamMapper, FederatedTeamSourcePrefix+"%").
		Delete(&dbModels.TeamMember{})
	if result.Error != nil {
		return ctx.Oops().Wrapf(result.Error, "failed to remove person %s from team %s", personID, teamID)
	} else if result.RowsAffected == 0 {
		return api.Errorf(api.ENOTFOUND, "person %s is not a direct member of team %s", personID, teamID)
	}
	return nil
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
