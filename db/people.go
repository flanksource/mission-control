package db

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm/clause"
)

func UpdateUserProperties(ctx *api.Context, userID string, newProps api.PersonProperties) error {
	var current api.Person
	if err := ctx.DB().Table("people").Where("id = ?", userID).First(&current).Error; err != nil {
		return err
	}

	props, err := utils.MergeStructs(current.Properties, newProps)
	if err != nil {
		return err
	}

	return ctx.DB().Table("people").Where("id = ?", userID).Update("properties", props).Error
}

func UpdateIdentityState(ctx *api.Context, id, state string) error {
	return ctx.DB().Table("identities").Where("id = ?", id).Update("state", state).Error
}

func GetOrCreateUser(ctx *api.Context, user api.Person) (api.Person, error) {
	if err := ctx.DB().Table("people").Where("email = ?", user.Email).Find(&user).Error; err != nil {
		return api.Person{}, err
	}
	if user.ID != uuid.Nil {
		return user, nil
	}
	err := ctx.DB().Table("people").Create(&user).Error
	return user, err
}

type CreateUserRequest struct {
	Username   string
	Password   string
	Properties models.PersonProperties
}

func CreatePerson(ctx *api.Context, name, personType string) (*models.Person, error) {
	person := models.Person{Name: name, Type: personType}
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
	keyLength   = 32
	saltLength  = 16
)

func CreateAccessToken(ctx *api.Context, personID uuid.UUID, name, password string, expiry time.Duration) (string, error) {
	saltRaw := make([]byte, saltLength)
	if _, err := crand.Read(saltRaw); err != nil {
		return "", err
	}
	salt := base64.RawStdEncoding.EncodeToString(saltRaw)

	hash := argon2.IDKey([]byte(password), []byte(salt), timeCost, memoryCost, parallelism, keyLength)
	encodedHash := base64.RawStdEncoding.EncodeToString(hash)

	accessToken := &models.AccessToken{
		Name:      fmt.Sprintf("agent-%d", time.Now().Unix()),
		Value:     encodedHash,
		PersonID:  personID,
		ExpiresAt: time.Now().Add(expiry), // long-lived token
	}
	if err := ctx.DB().Create(&accessToken).Error; err != nil {
		return "", err
	}

	formattedHash := fmt.Sprintf("%s.%s.%d.%d.%d", password, salt, timeCost, memoryCost, parallelism)
	return formattedHash, nil
}
