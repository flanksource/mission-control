package adapter

import (
	"strings"

	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The types of people, in people.type, other than humans, who have none.
const (
	PersonTypeAgent = "agent"

	// PersonTypeFederated is a user of an external application, authenticated by an ExternalIdentityProvider.
	PersonTypeFederated = "federated"

	// PersonTypeServiceAccount is a subject for automation, signed in only with its own access tokens
	// (specs/authorization/service-accounts.md).
	PersonTypeServiceAccount = "service_account"
)

// PersonTypes is a row of the allowlist of person types: the people a query that picks people accepts
// (specs/authorization/design/service-accounts.md, "Subjects are chosen by allowlist"). A type it doesn't name is
// never selected, including types added later.
type PersonTypes struct {
	// Humans accepts the people Mission Control's own authentication creates, which have no type, except the
	// System row, which has neither a type nor an email.
	Humans bool

	Types []string
}

// The allowlist of person types. Every query that picks people uses one of these rows.
var (
	// PeopleByEmail are the people a RoleBinding selects by email, or with people: ["*"].
	PeopleByEmail = PersonTypes{Humans: true}

	// FederatedPeople are the people a RoleBinding selects as <provider>:<username> or <provider>:*.
	FederatedPeople = PersonTypes{Types: []string{PersonTypeFederated}}

	// ServiceAccounts are the people a RoleBinding selects by serviceAccounts.
	ServiceAccounts = PersonTypes{Types: []string{PersonTypeServiceAccount}}

	// TeamMembers are the people a team can have as members.
	TeamMembers = PersonTypes{Humans: true, Types: []string{PersonTypeFederated}}

	// Logins are the people a login or session of Mission Control's own authentication signs in as.
	Logins = PersonTypes{Humans: true}

	// PersonOperations are the people a person operation, e.g. deleting a person, reaches.
	PersonOperations = PersonTypes{Humans: true, Types: []string{PersonTypeFederated}}

	// LimitedTokenOwners are the people who may own an access token limited by its bindings.
	LimitedTokenOwners = PersonTypes{Humans: true, Types: []string{PersonTypeServiceAccount}}

	// UnlimitedTokenOwners are the people who may own an access token that isn't limited.
	UnlimitedTokenOwners = PersonTypes{Types: []string{PersonTypeAgent}}
)

// Scope limits a query of the people table to the person types, e.g. db.Scopes(adapter.TeamMembers.Scope).
func (p PersonTypes) Scope(db *gorm.DB) *gorm.DB {
	var conditions []string
	var args []any
	if p.Humans {
		conditions = append(conditions, "(people.type IS NULL AND NOT (people.name = 'System' AND people.email IS NULL))")
	}
	if len(p.Types) > 0 {
		conditions = append(conditions, "people.type IN ?")
		args = append(args, p.Types)
	}
	if len(conditions) == 0 {
		return db.Where("FALSE")
	}
	return db.Where("("+strings.Join(conditions, " OR ")+")", args...)
}

// ApplyTeamMembership adds the person to the team in the policy, and reports whether it did. A person who can't be a
// team member is skipped, so the membership grants nothing however it was written
// (specs/authorization/design/service-accounts.md, "Team membership").
func ApplyTeamMembership(db *gorm.DB, personID, teamID string) (bool, error) {
	id, err := uuid.Parse(personID)
	if err != nil {
		return false, nil
	}
	if member, err := TeamMembers.Has(db, id); err != nil || !member {
		return false, err
	}
	return true, dutyRBAC.AddRoleForUser(id.String(), teamID)
}

// Has reports whether the person with the id, deleted or not, is of the person types.
func (p PersonTypes) Has(db *gorm.DB, id uuid.UUID) (bool, error) {
	var count int64
	err := db.Table("people").Where("people.id = ?", id).Scopes(p.Scope).Count(&count).Error
	return count > 0, err
}
