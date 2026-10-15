package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/flanksource/incident-commander/api"
	dbModels "github.com/flanksource/incident-commander/db/models"
)

const (
	// PersonTypeFederated is a user of an external application, authenticated by an ExternalIdentityProvider
	PersonTypeFederated = "federated"
)

// GetOrCreateFederatedPerson returns the person representing a user of an external identity provider,
// creating it on first sight. The person is keyed by <provider>:<username>.
//
// The claimed email is kept in the person's properties, never in people.email: it isn't verified
// by Mission Control, so it must not collide with or be matched against Mission Control users.
func GetOrCreateFederatedPerson(ctx context.Context, provider, username, name, email string) (*models.Person, error) {
	externalID := provider + ":" + username
	if name == "" {
		name = username
	}
	properties := models.PersonProperties{Provider: provider, Email: email}

	var person models.Person
	err := ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
		// Serializes concurrent first requests for the same user so only one person is created
		if err := ctx.DB().Exec("SELECT pg_advisory_xact_lock(hashtext(?))", externalID).Error; err != nil {
			return err
		}

		err := ctx.DB().Where("external_id = ? AND type = ? AND deleted_at IS NULL", externalID, PersonTypeFederated).First(&person).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			person = models.Person{Name: name, Type: PersonTypeFederated, ExternalID: externalID, Properties: properties}
			return ctx.DB().Create(&person).Error
		} else if err != nil {
			return err
		}

		if person.Name == name && person.Properties == properties {
			return nil
		}

		person.Name = name
		person.Properties = properties
		return ctx.DB().Model(&models.Person{}).Where("id = ?", person.ID).
			Updates(map[string]any{"name": name, "properties": properties}).Error
	})
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to get or create federated person %s", externalID)
	}

	return &person, nil
}

func UpdateUserProperties(ctx context.Context, userID string, newProps api.PersonProperties) error {
	var current api.Person
	if err := ctx.DB().Table("people").Where("id = ?", userID).First(&current).Error; err != nil {
		return err
	}

	props, err := collections.MergeStructs(current.Properties, newProps)
	if err != nil {
		return err
	}

	return ctx.DB().Table("people").Where("id = ?", userID).Update("properties", props).Error
}

func UpdateIdentityState(ctx context.Context, id, state string) error {
	return ctx.DB().Table("identities").Where("id = ?", id).Update("state", state).Error
}

func GetUserByID(ctx context.Context, id string) (models.Person, error) {
	var user models.Person
	err := ctx.DB().Table("people").Where("id = ?", id).First(&user).Error
	return user, err
}

// FindPerson returns the person, not deleted, with the given ID or email.
func FindPerson(ctx context.Context, idOrEmail string) (*models.Person, error) {
	query := ctx.DB().Where("deleted_at IS NULL")
	if id, err := uuid.Parse(idOrEmail); err == nil {
		query = query.Where("id = ?", id)
	} else {
		query = query.Where("email = ?", idOrEmail)
	}

	var person models.Person
	if err := query.First(&person).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	return &person, nil
}

func GetTeamsForUser(ctx context.Context, id string) ([]models.Team, error) {
	var teams []models.Team
	err := ctx.DB().Raw("SELECT teams.* FROM teams LEFT JOIN team_members ON teams.id = team_members.team_id WHERE team_members.person_id = ?", id).Scan(&teams).Error
	return teams, err
}

func GetUserByExternalID(ctx context.Context, id string) (models.Person, error) {
	var user models.Person
	err := ctx.DB().Table("people").Where("external_id = ?", id).First(&user).Error
	return user, err
}

// CreateUser creates a new user and returns a copy
func CreateUser(ctx context.Context, user models.Person) (models.Person, error) {
	err := ctx.DB().Table("people").Create(&user).Error
	return user, err
}

type CreateUserRequest struct {
	Username   string
	Password   string
	Properties models.PersonProperties
}

func CreatePerson(ctx context.Context, name, email, personType string) (*models.Person, error) {
	person := models.Person{Name: name, Email: email, Type: personType}
	if err := ctx.DB().Clauses(clause.Returning{}).Create(&person).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to create person %q", email)
	}

	return &person, nil
}

func UpdateAccessTokenExpiry(ctx context.Context, tokenID uuid.UUID, newExpiry time.Time) error {
	return ctx.DB().Model(&models.AccessToken{}).
		Where("id = ?", tokenID).
		Update("expires_at", newExpiry).
		Error
}

type AccessTokenWithUser struct {
	models.AccessToken
	Person models.Person `json:"person" gorm:"foreignKey:PersonID"`
}

// ListAccessTokens lists the person's own tokens: those they created that sign in as them.
func ListAccessTokens(ctx context.Context, owner uuid.UUID) ([]AccessTokenWithUser, error) {
	return gorm.G[AccessTokenWithUser](ctx.DB()).
		Select("id", "name", "person_id", "created_at", "roles").
		Where("person_id = ? AND created_by = ?", owner, owner).
		Preload("Person", nil).
		Find(ctx)
}

// ListPersonAccessTokens lists the access tokens of the person, e.g. an agent's, without their values.
func ListPersonAccessTokens(ctx context.Context, personID uuid.UUID) ([]models.AccessToken, error) {
	return gorm.G[models.AccessToken](ctx.DB()).
		Select("id", "name", "person_id", "created_at", "expires_at", "auto_renew").
		Where("person_id = ?", personID).
		Order("created_at").
		Find(ctx)
}

func GetAccessToken(ctx context.Context, id string) (models.AccessToken, error) {
	return gorm.G[models.AccessToken](ctx.DB()).
		Select("id", "name", "person_id", "created_at", "created_by").
		Where("id = ?", id).
		First(ctx)
}

// GetExpiredAccessTokens returns all access tokens whose expiry time has passed.
// Tokens with NULL expires_at (non-expiring tokens) are not included.
func GetExpiredAccessTokens(ctx context.Context) ([]models.AccessToken, error) {
	tokens, err := gorm.G[models.AccessToken](ctx.DB()).
		Where("expires_at < NOW()").
		Find(ctx)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to query expired access tokens")
	}
	return tokens, nil
}

// GetPersonType returns the type field of a person record by their ID.
// Returns an error if the person is not found.
func GetPersonType(ctx context.Context, id string) (string, error) {
	var person models.Person
	if err := ctx.DB().Table("people").Select("type").Where("id = ?", id).First(&person).Error; err != nil {
		return "", ctx.Oops().Wrapf(err, "failed to get person type for person %s", id)
	}
	return person.Type, nil
}

// DeleteAccessToken deletes the access token.
func DeleteAccessToken(ctx context.Context, id string) error {
	if err := ctx.DB().Where("id = ?", id).Delete(&models.AccessToken{}).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to delete access token %s", id)
	}
	return nil
}

func AddPersonToTeam(ctx context.Context, personID uuid.UUID, teamID uuid.UUID) error {
	return ctx.DB().
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&dbModels.TeamMember{PersonID: personID, TeamID: teamID}).
		Error
}

func UpdateLastLogin(ctx context.Context, id string) error {
	return ctx.DB().Table("people").Where("id = ?", id).UpdateColumn("last_login", "NOW()").Error
}

var SystemUser *models.Person

func GetSystemUser(ctx context.Context) (*models.Person, error) {
	if SystemUser == nil {
		if err := ctx.DB().Model(&models.Person{}).Where("name = 'System'").First(&SystemUser).Error; err != nil {
			return nil, fmt.Errorf("error fetching system user from database: %w", err)
		}
	}

	api.SystemUserID = lo.ToPtr(SystemUser.ID)
	return SystemUser, nil
}

// PersonAnalytic is an action a person has taken, e.g. one of the getting-started checklist's, and how many times.
type PersonAnalytic struct {
	Key       string    `json:"key"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListPersonAnalytics lists the actions the person has taken.
func ListPersonAnalytics(ctx context.Context, personID uuid.UUID) ([]PersonAnalytic, error) {
	var analytics []PersonAnalytic
	err := ctx.DB().Table("person_analytics").Select("key", "count", "created_at", "updated_at").
		Where("person_id = ?", personID).Order("key").Find(&analytics).Error
	return analytics, err
}

// RecordPersonAnalytic records that the person took the action. A person_analytics trigger counts repeats.
func RecordPersonAnalytic(ctx context.Context, personID uuid.UUID, key string) error {
	return ctx.DB().Exec("INSERT INTO person_analytics (person_id, key) VALUES (?, ?)", personID, key).Error
}
