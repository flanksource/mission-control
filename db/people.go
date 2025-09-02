package db

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PersonTypeAccessToken = "access_token"

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
		return nil, err
	}

	return &person, nil
}

const (
	// The draft RFC(https://tools.ietf.org/html/draft-irtf-cfrg-argon2-03#section-9.3) recommends
	// the following time and memory cost as sensible defaults.
	timeCost    = 1
	memoryCost  = 64 * 1024
	parallelism = 4
	keyLength   = 20
	saltLength  = 12
)

func CreateAccessToken(ctx context.Context, personID uuid.UUID, name, password string, expiry *time.Duration, createdBy *uuid.UUID) (string, *models.AccessToken, error) {
	saltRaw := make([]byte, saltLength)
	if _, err := crand.Read(saltRaw); err != nil {
		return "", nil, err
	}
	salt := base64.URLEncoding.EncodeToString(saltRaw)

	hash := argon2.IDKey([]byte(password), []byte(salt), timeCost, memoryCost, parallelism, keyLength)
	encodedHash := base64.URLEncoding.EncodeToString(hash)

	if name == "default" {
		name = fmt.Sprintf("agent-%d", time.Now().Unix())
	}

	accessToken := &models.AccessToken{
		Name:     name,
		Value:    encodedHash,
		PersonID: personID,
	}
	if expiry != nil {
		accessToken.ExpiresAt = lo.ToPtr(time.Now().Add(*expiry))
	}
	if createdBy != nil {
		accessToken.CreatedBy = createdBy
	}

	if err := ctx.DB().Create(&accessToken).Error; err != nil {
		return "", nil, err
	}

	formattedHash := fmt.Sprintf("%s.%s.%d.%d.%d", password, salt, timeCost, memoryCost, parallelism)
	return formattedHash, accessToken, nil
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

func ListAccessTokens(ctx context.Context) ([]AccessTokenWithUser, error) {
	return gorm.G[AccessTokenWithUser](ctx.DB()).
		Select("id", "name", "person_id", "created_at").
		Preload("Person", nil).
		Find(ctx)
}

func GetAccessToken(ctx context.Context, id string) (models.AccessToken, error) {
	return gorm.G[models.AccessToken](ctx.DB()).
		Select("id", "name", "person_id", "created_at").
		Where("id = ?", id).
		First(ctx)
}

func DeleteAccessToken(ctx context.Context, id string) error {
	_, err := gorm.G[models.AccessToken](ctx.DB()).
		Where("id = ?", id).
		Delete(ctx)
	return err
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
