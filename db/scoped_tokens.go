package db

import (
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// scopedAccessToken is an access token limited by deny rows: the way tokens were limited before they were limited by Roles.
type scopedAccessToken struct {
	ID         uuid.UUID
	Name       string
	PersonID   uuid.UUID
	OwnerName  string
	OwnerEmail string
}

// RevokeScopedAccessTokens deletes every access token limited by deny rows, with its person and its rows.
// Converting one would widen it to its owner's full access, so it's revoked instead. Unscoped tokens are untouched.
func RevokeScopedAccessTokens(ctx context.Context) error {
	var tokens []scopedAccessToken
	if err := ctx.DB().Raw(`
		SELECT access_tokens.id, access_tokens.name, access_tokens.person_id,
			COALESCE(owner.name, '') AS owner_name, COALESCE(owner.email, '') AS owner_email
		FROM access_tokens
		INNER JOIN people ON people.id = access_tokens.person_id AND people.type = ?
		LEFT JOIN people owner ON owner.id = access_tokens.created_by
		WHERE EXISTS (
			SELECT 1 FROM casbin_rule
			WHERE casbin_rule.ptype = 'p' AND casbin_rule.v0 = access_tokens.person_id::TEXT AND casbin_rule.v3 = 'deny'
		)`, PersonTypeAccessToken).Scan(&tokens).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to find scoped access tokens")
	}

	for _, token := range tokens {
		err := ctx.DB().Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`DELETE FROM casbin_rule WHERE ptype IN ('p', 'g') AND v0 = ?`, token.PersonID.String()).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", token.ID).Delete(&models.AccessToken{}).Error; err != nil {
				return err
			}
			return tx.Model(&models.Person{}).Where("id = ?", token.PersonID).UpdateColumn("deleted_at", duty.Now()).Error
		})
		if err != nil {
			return ctx.Oops().Wrapf(err, "failed to revoke scoped access token %s", token.ID)
		}

		ctx.Warnf("revoked scoped access token %q (id=%s) of %s <%s>: tokens are now limited by Roles; create a new token with roles",
			token.Name, token.ID, token.OwnerName, token.OwnerEmail)
	}

	return nil
}
