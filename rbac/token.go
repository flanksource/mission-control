package rbac

import (
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

type accessTokenKey struct{}

// requestToken is the access token a request was made with.
type requestToken struct {
	id     uuid.UUID
	person string
	roles  models.AccessTokenRoles
}

// WithAccessToken marks the request as made with the access token. The token signs in as its person, and its Roles
// limit the checks made as that person during the request (TokenLimit).
func WithAccessToken(ctx context.Context, token *models.AccessToken) context.Context {
	return ctx.WithValue(accessTokenKey{}, requestToken{id: token.ID, person: token.PersonID.String(), roles: token.Roles})
}

// RequestAccessTokenID returns the ID of the access token the request was made with, or nil.
func RequestAccessTokenID(ctx context.Context) *uuid.UUID {
	if token, ok := ctx.Value(accessTokenKey{}).(requestToken); ok {
		return &token.id
	}
	return nil
}

// MadeWithAccessToken reports whether the request was authenticated by an access token.
func MadeWithAccessToken(ctx context.Context) bool {
	_, ok := ctx.Value(accessTokenKey{}).(requestToken)
	return ok
}

// AccessTokenCacheKey tells apart what's cached for a subject by the access token the request was made with:
// a person's sessions and each of their tokens share the subject, but not their limits.
// It's empty for a request made without a token.
func AccessTokenCacheKey(ctx context.Context) string {
	if token, ok := ctx.Value(accessTokenKey{}).(requestToken); ok {
		return AccessTokenIDCacheKey(token.id)
	}
	return ""
}

// AccessTokenIDCacheKey is AccessTokenCacheKey of a request made with the access token.
func AccessTokenIDCacheKey(id uuid.UUID) string {
	return ":token=" + id.String()
}

// ForgetAccessToken drops what's cached for requests made with the access token, e.g. once it's deleted.
func ForgetAccessToken(id uuid.UUID) {
	key := AccessTokenIDCacheKey(id)
	for cacheKey := range summaryCache.Items() {
		if strings.Contains(cacheKey, key) {
			summaryCache.Delete(cacheKey)
		}
	}
}

// TokenLimit returns the Roles that limit a check made as the subject, and whether it's limited.
// A check is limited when the request was made with an access token that lists Roles and the subject is the
// request's own user, the token's person. Checks made as another subject, e.g. a playbook during a run the request
// started, aren't limited, and neither are requests made without a token. A token that lists no Roles, e.g. an
// agent's, doesn't limit.
func TokenLimit(ctx context.Context, subject string) (models.AccessTokenRoles, bool) {
	token, ok := ctx.Value(accessTokenKey{}).(requestToken)
	if !ok || len(token.roles) == 0 || subject != token.person {
		return nil, false
	}
	return token.roles, true
}

// withinTokenLimit reports whether the request's access token allows a check as the subject: it isn't limited,
// or allowed accepts the limit principal of one of its Roles.
func withinTokenLimit(ctx context.Context, subject string, allowed func(principal string) bool) bool {
	roles, limited := TokenLimit(ctx, subject)
	if !limited {
		return true
	}

	return lo.SomeBy(roles, func(role models.AccessTokenRole) bool {
		return allowed(adapter.RoleLimitPrincipal(role.Namespace, role.Name))
	})
}

// CheckContext checks the action on a whole object as ctx.Subject(), limited by the Roles of the request's access
// token. Every subject is checked the same way, whatever kind it is (specs/authorization/overview.md, "Access").
func CheckContext(ctx context.Context, object, action string) bool {
	user := ctx.User()
	if user == nil {
		return false
	}
	return rbac.Check(ctx, ctx.Subject(), object, action) && TokenAllowsObject(ctx, ctx.Subject(), object, action)
}

// HasPermission is rbac.HasPermission, limited by the Roles of the request's access token when the subject is the
// request's own user. Checks made as another subject, e.g. a playbook during its run, aren't limited.
func HasPermission(ctx context.Context, subject string, attr *models.ABACAttribute, action string) bool {
	return rbac.HasPermission(ctx, subject, attr, action) && TokenAllows(ctx, subject, attr, action)
}

// TokenAllowsObject reports whether the Roles of the request's access token allow a check as the subject on a whole
// object. It's true when the check isn't limited by a token (TokenLimit).
func TokenAllowsObject(ctx context.Context, subject, object, action string) bool {
	return withinTokenLimit(ctx, subject, func(principal string) bool {
		allowed, err := rbac.Enforcer().Enforce(principal, object, action)
		if err != nil {
			ctx.Errorf("failed to check %s:%s against %s: %v", object, action, principal, err)
		}
		return err == nil && allowed
	})
}

// TokenAllows reports whether the Roles of the request's access token allow a check as the subject on resources.
// It's true when the check isn't limited by a token (TokenLimit).
func TokenAllows(ctx context.Context, subject string, attr *models.ABACAttribute, action string) bool {
	return withinTokenLimit(ctx, subject, func(principal string) bool {
		return rbac.HasPermission(ctx, principal, attr, action)
	})
}

// ActsAsPerson reports whether the request is checked as its person. A user of an external identity provider is
// checked as the bindings their token's claims match instead, so a credential that authenticates as the person,
// e.g. an access token, would give them grants they don't have.
func ActsAsPerson(ctx context.Context) bool {
	user := ctx.User()
	return user != nil && ctx.Subject() == user.ID.String()
}
