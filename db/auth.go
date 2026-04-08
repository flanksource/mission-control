package db

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/properties"
	"github.com/flanksource/commons/rand"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/argon2"
)

// CreateAccessToken generates a new access token using Argon2id hashing.
// Returns: "password.salt.timeCost.memoryCost.parallelism" to user, stores base64(hash) in DB.
func CreateAccessToken(ctx context.Context, personID uuid.UUID, name, password string, expiry *time.Duration, createdBy *uuid.UUID, autoRenew bool) (string, *models.AccessToken, error) {
	saltRaw := make([]byte, saltLength)
	if _, err := crand.Read(saltRaw); err != nil {
		return "", nil, ctx.Oops().Wrapf(err, "failed to generate salt")
	}
	salt := base64.URLEncoding.EncodeToString(saltRaw)

	hash := argon2.IDKey([]byte(password), []byte(salt), timeCost, memoryCost, parallelism, keyLength)
	encodedHash := base64.URLEncoding.EncodeToString(hash)

	if name == "default" {
		name = fmt.Sprintf("agent-%d", time.Now().Unix())
	}

	accessToken := &models.AccessToken{
		Name:      name,
		Value:     encodedHash,
		PersonID:  personID,
		AutoRenew: autoRenew,
	}
	if expiry != nil {
		accessToken.ExpiresAt = lo.ToPtr(time.Now().Add(*expiry))
	}
	if createdBy != nil {
		accessToken.CreatedBy = createdBy
	}

	if err := ctx.DB().Create(&accessToken).Error; err != nil {
		return "", nil, ctx.Oops().Wrapf(err, "failed to create access token")
	}

	formattedHash := fmt.Sprintf("%s.%s.%d.%d.%d", password, salt, timeCost, memoryCost, parallelism)
	return formattedHash, accessToken, nil
}

type CreateAccessTokenForPersonResult struct {
	Token  string
	Person *models.Person
}

func CreateAccessTokenForPerson(ctx context.Context, user *models.Person, tokenName string, expiry time.Duration, autoRenew bool) (CreateAccessTokenForPersonResult, error) {
	var output CreateAccessTokenForPersonResult
	err := ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
		name := user.Name + " (Token)"
		emailParts := strings.Split(user.Email, "@")
		if len(emailParts) != 2 {
			return ctx.Oops().Code(dutyAPI.EINVALID).Errorf("invalid email %q", user.Email)
		}

		email := emailParts[0] + "+" + "token:" + tokenName + "@" + emailParts[1]

		person, err := CreatePerson(ctx, name, email, PersonTypeAccessToken)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to create person for token %q", tokenName)
		}

		password, err := rand.GenerateRandHex(32)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to generate password for token %q", tokenName)
		}

		// 0 expiry means default
		if expiry == 0 {
			expiry = properties.Duration(90*24*time.Hour, "access_token.default_expiry")
		}

		token, _, err := CreateAccessToken(ctx, person.ID, tokenName, password, &expiry, new(user.ID), autoRenew)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to create access token %q", tokenName)
		}

		output = CreateAccessTokenForPersonResult{
			Token:  token,
			Person: person,
		}
		return nil
	})

	return output, err
}
