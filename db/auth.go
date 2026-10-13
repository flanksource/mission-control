package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/properties"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/secret"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"

	"github.com/flanksource/incident-commander/auth/accesstoken"
)

// CreateAccessToken generates a new v2 access token and saves it in the database.
// A person's token must list at least one Role. An agent's lists none: Roles don't apply to agents
// (specs/authorization/roles.md, Section 4.5).
func CreateAccessToken(ctx context.Context, personID uuid.UUID, name string, expiry *time.Duration, createdBy *uuid.UUID, autoRenew bool, roles models.AccessTokenRoles) (secret.Sensitive, *models.AccessToken, error) {
	personType, err := GetPersonType(ctx, personID.String())
	if err != nil {
		return nil, nil, err
	}
	if personType == PersonTypeAgent && len(roles) > 0 {
		return nil, nil, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("an agent's access token can't list roles")
	} else if personType != PersonTypeAgent && len(roles) == 0 {
		return nil, nil, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("an access token must list at least one role")
	}

	token, err := accesstoken.Generate()
	if err != nil {
		return nil, nil, err
	}

	if name == "default" {
		name = fmt.Sprintf("agent-%d", time.Now().Unix())
	}

	accessTokenModel := &models.AccessToken{
		Name:      name,
		Value:     token.Hash(),
		PersonID:  personID,
		AutoRenew: autoRenew,
		Roles:     roles,
	}
	if expiry != nil {
		accessTokenModel.ExpiresAt = lo.ToPtr(time.Now().Add(*expiry))
	}
	if createdBy != nil {
		accessTokenModel.CreatedBy = createdBy
	}

	if err := ctx.DB().Create(&accessTokenModel).Error; err != nil {
		return nil, nil, ctx.Oops().Wrapf(err, "failed to create access token")
	}

	return token.V2(), accessTokenModel, nil
}

// PersonalTokensWithoutRoles returns the access tokens that list no Role and don't belong to an agent.
// Tokens created before Roles were required are such tokens.
func PersonalTokensWithoutRoles(ctx context.Context) ([]models.AccessToken, error) {
	var tokens []models.AccessToken
	err := ctx.DB().Raw(`SELECT t.* FROM access_tokens t LEFT JOIN people p ON p.id = t.person_id
		WHERE p.type IS DISTINCT FROM ?
			AND COALESCE(jsonb_typeof(t.roles) <> 'array' OR t.roles = '[]'::jsonb, TRUE)`, PersonTypeAgent).
		Scan(&tokens).Error
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list access tokens without roles")
	}
	return tokens, nil
}

type CreateAccessTokenForPersonResult struct {
	Token  secret.Sensitive
	Person *models.Person
}

// CreateAccessTokenForPerson creates an access token of the user, limited by the given Roles.
func CreateAccessTokenForPerson(ctx context.Context, user *models.Person, tokenName string, expiry time.Duration, autoRenew bool, roles models.AccessTokenRoles) (CreateAccessTokenForPersonResult, error) {
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

		// 0 expiry means default
		if expiry == 0 {
			expiry = properties.Duration(90*24*time.Hour, "access_token.default_expiry")
		}

		token, _, err := CreateAccessToken(ctx, person.ID, tokenName, &expiry, new(user.ID), autoRenew, roles)
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
