package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/secret"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"

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

// CreateAccessTokenForPerson creates an access token of the user, limited by the given Roles. The token signs in as
// the user: it's no subject of its own.
func CreateAccessTokenForPerson(ctx context.Context, user *models.Person, tokenName string, expiry time.Duration, autoRenew bool, roles models.AccessTokenRoles) (secret.Sensitive, *models.AccessToken, error) {
	// 0 expiry means default
	if expiry == 0 {
		expiry = properties.Duration(90*24*time.Hour, "access_token.default_expiry")
	}

	token, accessToken, err := CreateAccessToken(ctx, user.ID, tokenName, &expiry, new(user.ID), autoRenew, roles)
	if err != nil {
		return nil, nil, ctx.Oops().Wrapf(err, "failed to create access token %q", tokenName)
	}
	return token, accessToken, nil
}

// legacyPersonTypeAccessToken is the type of the people access tokens were created for before tokens signed in as
// their owners.
const legacyPersonTypeAccessToken = "access_token"

// LegacyTokenPersonMigration is what MigrateLegacyTokenPeople did to an access token of a person of its own.
type LegacyTokenPersonMigration struct {
	Token models.AccessToken

	// Person is the token's former person, which was soft-deleted.
	Person uuid.UUID

	// Deleted is set when the token was deleted, rather than moved to its owner, since it names no owner.
	Deleted bool

	// Renamed is the token's new name when its owner already had a token of its name, or empty.
	Renamed string
}

// MigrateLegacyTokenPeople moves every access token of a person of type access_token to its owner, created_by,
// soft-deletes that person and removes its rules from casbin_rule. A token without an owner is deleted, and a token
// whose name its owner already uses is renamed.
// It's idempotent: each token is migrated in a transaction, and a migrated token's person is its owner.
func MigrateLegacyTokenPeople(ctx context.Context) ([]LegacyTokenPersonMigration, error) {
	var tokens []models.AccessToken
	if err := ctx.DB().Raw(`SELECT t.* FROM access_tokens t JOIN people p ON p.id = t.person_id WHERE p.type = ?`,
		legacyPersonTypeAccessToken).Scan(&tokens).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list the access tokens of token people")
	}

	var migrated []LegacyTokenPersonMigration
	var errs []error
	for _, token := range tokens {
		result := LegacyTokenPersonMigration{Token: token, Person: token.PersonID, Deleted: token.CreatedBy == nil}
		err := ctx.DB().Transaction(func(tx *gorm.DB) error {
			if result.Deleted {
				if err := tx.Where("id = ?", token.ID).Delete(&models.AccessToken{}).Error; err != nil {
					return err
				}
			} else {
				// A person's token names are unique, so a name the owner already uses would stop the move for good
				var clashes int64
				if err := tx.Model(&models.AccessToken{}).Where("person_id = ? AND name = ? AND id <> ?", token.CreatedBy, token.Name, token.ID).
					Count(&clashes).Error; err != nil {
					return err
				}
				updates := map[string]any{"person_id": token.CreatedBy}
				if clashes > 0 {
					result.Renamed = fmt.Sprintf("%s (migrated %s)", token.Name, token.ID)
					updates["name"] = result.Renamed
				}
				if err := tx.Model(&models.AccessToken{}).Where("id = ?", token.ID).UpdateColumns(updates).Error; err != nil {
					return err
				}
			}

			if err := tx.Model(&models.Person{}).Where("id = ? AND type = ? AND deleted_at IS NULL", token.PersonID, legacyPersonTypeAccessToken).
				UpdateColumn("deleted_at", duty.Now()).Error; err != nil {
				return err
			}

			return tx.Exec("DELETE FROM casbin_rule WHERE v0 = ?", token.PersonID.String()).Error
		})
		if err != nil {
			errs = append(errs, ctx.Oops().Wrapf(err, "failed to migrate access token %s of token person %s", token.ID, token.PersonID))
			continue
		}
		migrated = append(migrated, result)
	}
	return migrated, errors.Join(errs...)
}
