package db

import (
	"errors"
	"time"

	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/secret"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersonTypeServiceAccount is the type of the people that are service accounts.
const PersonTypeServiceAccount = adapter.PersonTypeServiceAccount

// ServiceAccount is a subject for automation, stored as a row of people (specs/authorization/service-accounts.md).
//
// Who created it isn't recorded yet: people has no created_by column until duty adds one.
type ServiceAccount struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (ServiceAccount) TableName() string {
	return "people"
}

// ServiceAccountToken is what's listed of a service account's token: never its value.
type ServiceAccountToken struct {
	models.AccessToken

	// Issuer is whoever issued the token.
	Issuer *models.Person `json:"issuer,omitempty" gorm:"foreignKey:CreatedBy"`

	// RoleBindings are the bindings that select the token, which limit it.
	RoleBindings []AccessTokenBinding `json:"role_bindings" gorm:"-"`
}

func serviceAccounts(ctx context.Context) *gorm.DB {
	return ctx.DB().Model(&ServiceAccount{}).Select("id", "name", "created_at").
		Where("deleted_at IS NULL").Scopes(adapter.ServiceAccounts.Scope)
}

// ListServiceAccounts lists the service accounts, by name.
func ListServiceAccounts(ctx context.Context) ([]ServiceAccount, error) {
	var accounts []ServiceAccount
	if err := serviceAccounts(ctx).Order("name").Find(&accounts).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list service accounts")
	}
	return accounts, nil
}

// FindServiceAccount returns the service account with the name, or ENOTFOUND.
func FindServiceAccount(ctx context.Context, name string) (*ServiceAccount, error) {
	var account ServiceAccount
	if err := serviceAccounts(ctx).Where("name = ?", name).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "service account %s not found", name)
		}
		return nil, ctx.Oops().Wrapf(err, "failed to get service account %s", name)
	}
	return &account, nil
}

// CreateServiceAccount creates a service account with no access and no token. Its name must be a valid service
// account name that no other service account has.
func CreateServiceAccount(ctx context.Context, name string) (*ServiceAccount, error) {
	if err := v1.ValidateServiceAccountName(name); err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "service account name %v", err)
	}

	var account ServiceAccount
	err := ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		// Names are unique among service accounts. Until duty indexes them, creations of a name are serialized here.
		if err := txCtx.DB().Exec("SELECT pg_advisory_xact_lock(hashtext(?))", PersonTypeServiceAccount+"/"+name).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to lock service account name %s", name)
		}

		var taken int64
		if err := serviceAccounts(txCtx).Where("name = ?", name).Count(&taken).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to get service account %s", name)
		} else if taken > 0 {
			return dutyAPI.Errorf(dutyAPI.ECONFLICT, "service account %s already exists", name)
		}

		person := models.Person{Name: name, Type: PersonTypeServiceAccount}
		if err := txCtx.DB().Create(&person).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to create service account %s", name)
		}
		return serviceAccounts(txCtx).Where("id = ?", person.ID).First(&account).Error
	})
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// DeleteServiceAccount soft-deletes the service account and deletes its tokens and their bindings, all or none.
// revoked is called with the ids of the tokens before the deletion commits.
func DeleteServiceAccount(ctx context.Context, account ServiceAccount, revoked func(tokenIDs []string)) error {
	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		// Locked so that a token issued meanwhile either commits first, and is deleted here, or finds no owner
		var locked []uuid.UUID
		if err := txCtx.DB().Raw("SELECT id FROM people WHERE id = ? AND deleted_at IS NULL FOR UPDATE", account.ID).Scan(&locked).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to lock service account %s", account.Name)
		} else if len(locked) == 0 {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "service account %s not found", account.Name)
		}

		var tokenIDs []string
		if err := txCtx.DB().Model(&models.AccessToken{}).Where("person_id = ?", account.ID).Pluck("id::text", &tokenIDs).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to list the access tokens of service account %s", account.Name)
		}

		if err := deleteAccessTokens(txCtx, tokenIDs...); err != nil {
			return err
		}

		if err := txCtx.DB().Model(&models.Person{}).Where("id = ?", account.ID).Update("deleted_at", duty.Now()).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to delete service account %s", account.Name)
		}

		revoked(tokenIDs)
		return nil
	})
}

// CreateServiceAccountToken issues a token of the service account, limited by the Roles, with the issuer recorded as
// its creator. Without an expiry, it expires after the default expiry.
func CreateServiceAccountToken(ctx context.Context, account ServiceAccount, issuer uuid.UUID, name string, expiry time.Duration, autoRenew bool, roles AccessTokenRoles) (secret.Sensitive, *models.AccessToken, error) {
	if name == "" {
		return nil, nil, dutyAPI.Errorf(dutyAPI.EINVALID, "a token name is required")
	}

	var taken int64
	if err := ctx.DB().Model(&models.AccessToken{}).Where("person_id = ? AND name = ?", account.ID, name).Count(&taken).Error; err != nil {
		return nil, nil, ctx.Oops().Wrapf(err, "failed to get the tokens of service account %s", account.Name)
	} else if taken > 0 {
		return nil, nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "service account %s already has a token named %s", account.Name, name)
	}

	expiry = tokenExpiry(expiry)
	return CreateAccessToken(ctx, account.ID, name, &expiry, &issuer, autoRenew, roles)
}

// ListServiceAccountTokens lists the tokens of the service account, with their Roles and issuers.
func ListServiceAccountTokens(ctx context.Context, account ServiceAccount) ([]ServiceAccountToken, error) {
	var tokens []ServiceAccountToken
	if err := ctx.DB().
		Select("id", "name", "person_id", "created_at", "created_by", "expires_at", "auto_renew").
		Where("person_id = ?", account.ID).
		Preload("Issuer").
		Order("created_at").
		Find(&tokens).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list the tokens of service account %s", account.Name)
	}

	for i, token := range tokens {
		bindings, err := accessTokenBindings(ctx, token.ID)
		if err != nil {
			return nil, err
		}
		tokens[i].RoleBindings = bindings
	}
	return tokens, nil
}

// ServiceAccountRoleBindings returns the RoleBindings that name the service account in serviceAccounts.
func ServiceAccountRoleBindings(ctx context.Context, name string) ([]models.RoleBinding, error) {
	var bindings []models.RoleBinding
	if err := ctx.DB().Where("deleted_at IS NULL").
		Where("subjects->'serviceAccounts' @> jsonb_build_array(?::text)", name).
		Order("namespace, name").
		Find(&bindings).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list the role bindings of service account %s", name)
	}
	return bindings, nil
}
