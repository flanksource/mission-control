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
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth/accesstoken"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// AccessTokenRole names a Role, by namespace and name, that limits an access token it's created with.
type AccessTokenRole struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// AccessTokenRoles are the Roles an access token is created with.
type AccessTokenRoles []AccessTokenRole

// CreateAccessToken generates a new v2 access token and saves it in the database.
//
// A person's token must be created with at least one Role. Each becomes a RoleBinding that selects the token, which
// limits it (specs/authorization/limiting-permissions.md). An agent's is created with none: it isn't limited
// (specs/authorization/identities.md, Section 5).
func CreateAccessToken(ctx context.Context, personID uuid.UUID, name string, expiry *time.Duration, createdBy *uuid.UUID, autoRenew bool, roles AccessTokenRoles) (secret.Sensitive, *models.AccessToken, error) {
	unlimited, err := adapter.UnlimitedTokenOwners.Has(ctx.DB(), personID)
	if err != nil {
		return nil, nil, ctx.Oops().Wrapf(err, "failed to get person %s", personID)
	}
	if !unlimited {
		if limited, err := adapter.LimitedTokenOwners.Has(ctx.DB(), personID); err != nil {
			return nil, nil, ctx.Oops().Wrapf(err, "failed to get person %s", personID)
		} else if !limited {
			return nil, nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("person %s can't own an access token", personID)
		}
	}

	// A Role listed twice binds it once
	roles = lo.Uniq(roles)

	if unlimited && len(roles) > 0 {
		return nil, nil, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("an agent's access token can't list roles")
	} else if !unlimited && len(roles) == 0 {
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
	}
	if expiry != nil {
		accessTokenModel.ExpiresAt = lo.ToPtr(time.Now().Add(*expiry))
	}
	if createdBy != nil {
		accessTokenModel.CreatedBy = createdBy
	}

	// Whoever issued the token writes its bindings: its owner, or for a service account, the issuer
	bindingCreator := personID
	if createdBy != nil {
		bindingCreator = *createdBy
	}

	err = ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		// Locked so that deleting the owner, e.g. a service account with its tokens, can't miss this one
		var owners int64
		if err := txCtx.DB().Raw("SELECT COUNT(*) FROM (SELECT 1 FROM people WHERE id = ? AND deleted_at IS NULL FOR SHARE) owner", personID).
			Scan(&owners).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to get person %s", personID)
		} else if owners == 0 {
			return ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("person %s not found", personID)
		}

		if err := txCtx.DB().Create(&accessTokenModel).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to create access token")
		}

		for _, role := range roles {
			if err := createAccessTokenBinding(txCtx, accessTokenModel, role, bindingCreator); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// The token is committed, and its plain text exists only here, so a failed reload doesn't fail its creation:
	// the reload its bindings' insert notifies picks it up.
	if len(roles) > 0 {
		if err := adapter.ReloadPolicy(); err != nil {
			ctx.Errorf("failed to reload the policy after creating access token %q: %v", name, err)
		}
	}

	return token.V2(), accessTokenModel, nil
}

// SetAccessTokenRoles sets the Roles the access token is bound to, written by writer: Roles new to the list are bound,
// and the bindings of Roles left out of it are soft-deleted, in one transaction. The token is locked first, as
// deleting it locks it, and must not have expired and must be one owns accepts; otherwise it's not found. changed is
// false when the token was already bound to exactly these Roles (specs/authorization/token-bindings.md, Section 2).
func SetAccessTokenRoles(ctx context.Context, tokenID uuid.UUID, owns func(models.AccessToken) bool, roles AccessTokenRoles, writer uuid.UUID) (token models.AccessToken, changed bool, err error) {
	roles = lo.Uniq(roles)
	err = ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		if err := txCtx.DB().Raw(`SELECT id, name, person_id, created_by FROM access_tokens
			WHERE id = ? AND (expires_at IS NULL OR expires_at > NOW()) FOR UPDATE`, tokenID).
			Scan(&token).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to get access token %s", tokenID)
		} else if token.ID == uuid.Nil || !owns(token) {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "access token %s not found", tokenID)
		}

		bound, err := adapter.AccessTokenBindings(txCtx, tokenID)
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to get the role bindings of access token %q", token.Name)
		}

		wanted := lo.SliceToMap(roles, func(r AccessTokenRole) (string, AccessTokenRole) { return r.Namespace + "/" + r.Name, r })
		var removed []uuid.UUID
		for _, binding := range bound {
			if _, keep := wanted[binding.Role]; keep {
				delete(wanted, binding.Role)
			} else {
				removed = append(removed, binding.ID)
			}
		}

		if len(removed) > 0 {
			if err := txCtx.DB().Model(&models.RoleBinding{}).Where("id IN ?", removed).
				Update("deleted_at", duty.Now()).Error; err != nil {
				return ctx.Oops().Wrapf(err, "failed to remove roles from access token %q", token.Name)
			}
		}
		for _, role := range roles {
			if _, add := wanted[role.Namespace+"/"+role.Name]; add {
				if err := createAccessTokenBinding(txCtx, &token, role, writer); err != nil {
					return err
				}
			}
		}
		changed = len(removed) > 0 || len(wanted) > 0
		return nil
	})
	return token, changed, err
}

// createAccessTokenBinding creates the RoleBinding that limits the access token by the Role, in the reserved
// namespace of access tokens' bindings, written by whoever issued the token. Its only subject is the token, which no
// custom resource or RoleBinding API write can express, so it's stored directly.
func createAccessTokenBinding(ctx context.Context, token *models.AccessToken, role AccessTokenRole, issuer uuid.UUID) error {
	subjects, err := adapter.AccessTokenBindingSubjects(token.ID)
	if err != nil {
		return err
	}

	binding := models.RoleBinding{
		ID:          uuid.New(),
		Name:        adapter.AccessTokenBindingName(token.ID, role.Namespace, role.Name),
		Namespace:   adapter.AccessTokenNamespace,
		Description: fmt.Sprintf("Limits access token %s", token.Name),
		Source:      models.SourceUI,
		Role:        role.Namespace + "/" + role.Name,
		Subjects:    subjects,
		CreatedBy:   &issuer,
	}

	// A Role that's missing or invalid is stored as not ready, as for any binding: it limits the token to nothing.
	_, validationErr := adapter.ValidateBinding(ctx, nil, binding)
	if binding.Error, binding.ErrorReason, err = validity(validationErr); err != nil {
		return err
	}

	if err := ctx.DB().Create(&binding).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to bind role %s/%s to access token %q", role.Namespace, role.Name, token.Name)
	}
	return nil
}

// CreateAccessTokenForPerson creates an access token of the user, limited by the given Roles. The token signs in as
// the user: it's no subject of its own.
func CreateAccessTokenForPerson(ctx context.Context, user *models.Person, tokenName string, expiry time.Duration, autoRenew bool, roles AccessTokenRoles) (secret.Sensitive, *models.AccessToken, error) {
	expiry = tokenExpiry(expiry)
	token, accessToken, err := CreateAccessToken(ctx, user.ID, tokenName, &expiry, new(user.ID), autoRenew, roles)
	if err != nil {
		return nil, nil, ctx.Oops().Wrapf(err, "failed to create access token %q", tokenName)
	}
	return token, accessToken, nil
}

// tokenExpiry returns the expiry of a token created through the API: the given one, or the default when it's 0.
func tokenExpiry(expiry time.Duration) time.Duration {
	if expiry == 0 {
		return properties.Duration(90*24*time.Hour, "access_token.default_expiry")
	}
	return expiry
}

// legacyPersonTypeAccessToken is the type of the people access tokens were created for before tokens signed in as
// their owners.
const legacyPersonTypeAccessToken = "access_token"

// LegacyTokenPersonMigration is what MigrateLegacyTokenPeople did to an access token of a person of its own.
type LegacyTokenPersonMigration struct {
	Token models.AccessToken

	// Person is the token's former person, which was soft-deleted.
	Person uuid.UUID

	// Deleted is set when the token was deleted, rather than moved to its owner, since it names no owner that may own
	// a token: created_by is empty, or a person that's deleted or isn't a human, e.g. an agent or another token's
	// person.
	Deleted bool

	// Renamed is the token's new name when its owner already had a token of its name, or empty.
	Renamed string
}

// MigrateLegacyTokenPeople moves every access token of a person of type access_token to its owner, created_by,
// soft-deletes that person and removes its rules from casbin_rule. A token without an owner who may own one is
// deleted, and a token whose name its owner already uses is renamed.
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
		result := LegacyTokenPersonMigration{Token: token, Person: token.PersonID}
		err := ctx.DB().Transaction(func(tx *gorm.DB) error {
			owner, err := isTokenOwner(tx, token.CreatedBy)
			if err != nil {
				return err
			}
			result.Deleted = !owner

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

// isTokenOwner reports whether the person may own a token limited by its bindings, and isn't deleted.
func isTokenOwner(tx *gorm.DB, id *uuid.UUID) (bool, error) {
	if id == nil {
		return false, nil
	}
	var count int64
	err := tx.Model(&models.Person{}).
		Where("id = ? AND deleted_at IS NULL", *id).
		Scopes(adapter.LimitedTokenOwners.Scope).
		Count(&count).Error
	return count > 0, err
}

// SoftDeleteLegacyTokenPeople soft-deletes the people of type access_token that no access token belongs to any more,
// e.g. one whose token was deleted before its person, and removes their rules from casbin_rule. It returns their ids.
func SoftDeleteLegacyTokenPeople(ctx context.Context) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := ctx.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Person{}).
			Where("type = ? AND deleted_at IS NULL", legacyPersonTypeAccessToken).
			Where("NOT EXISTS (SELECT 1 FROM access_tokens t WHERE t.person_id = people.id)").
			Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
			return err
		}
		if err := tx.Model(&models.Person{}).Where("id IN ?", ids).UpdateColumn("deleted_at", duty.Now()).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM casbin_rule WHERE v0 IN ?", lo.Map(ids, func(id uuid.UUID, _ int) string { return id.String() })).Error
	})
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to soft-delete the people of deleted access tokens")
	}
	return ids, nil
}
