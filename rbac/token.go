package rbac

import (
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
)

type accessTokenKey struct{}

// requestToken is the access token a request was made with.
type requestToken struct {
	id     uuid.UUID
	person string
}

// WithAccessToken marks the request as made with the access token. The token signs in as its person, and the
// RoleBindings that select it limit the checks made as that person during the request (TokenLimit).
func WithAccessToken(ctx context.Context, token *models.AccessToken) context.Context {
	return ctx.WithValue(accessTokenKey{}, requestToken{id: token.ID, person: token.PersonID.String()})
}

// RequestAccessTokenID returns the ID of the access token the request was made with, or nil.
func RequestAccessTokenID(ctx context.Context) *uuid.UUID {
	if token, ok := ctx.Value(accessTokenKey{}).(requestToken); ok {
		return &token.id
	}
	return nil
}

// WithRequestIdentity returns `to` checked as the request in `from` is: as its user and subject, and limited by its
// access token. Use it whenever a request's work continues on a context that isn't derived from the request's,
// e.g. in a goroutine with a context of its own. Setting only the user would drop the token's limit.
func WithRequestIdentity(to, from context.Context) context.Context {
	user := from.User()
	if user != nil {
		to = to.WithUser(user)
	}
	if subject := from.Subject(); subject != "" && (user == nil || subject != user.ID.String()) {
		to = to.WithSubject(subject)
	}
	if token, ok := from.Value(accessTokenKey{}).(requestToken); ok {
		to = to.WithValue(accessTokenKey{}, token)
	}
	return to
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

// TokenLimit returns the access token that limits a check made as the subject, and whether it's limited.
// A check is limited when the request was made with an access token and the subject is the request's own user, the
// token's person. Checks made as another subject, e.g. a playbook during a run the request started, aren't limited,
// and neither are requests made without a token.
//
// Only an agent's token, on a request made as that agent, isn't limited. Any other token is limited by the
// RoleBindings that select it, so a token no binding selects may do nothing.
func TokenLimit(ctx context.Context, subject string) (uuid.UUID, bool) {
	token, ok := ctx.Value(accessTokenKey{}).(requestToken)
	if !ok || subject != token.person {
		return uuid.Nil, false
	}
	if agent := ctx.Agent(); agent != nil && agent.PersonID != nil && agent.PersonID.String() == token.person {
		return uuid.Nil, false
	}
	return token.id, true
}

// withinTokenLimit reports whether the request's access token allows a check as the subject: it isn't limited,
// or allowed accepts the token's principal, which the RoleBindings that select it are grouped under.
func withinTokenLimit(ctx context.Context, subject string, allowed func(principal string) bool) bool {
	tokenID, limited := TokenLimit(ctx, subject)
	if !limited {
		return true
	}

	return allowed(models.AccessTokenPrincipal(tokenID))
}

// CheckContext checks the action on a whole object as ctx.Subject(), limited by the request's access token. Every subject is checked the same way, whatever kind it is (specs/authorization/overview.md, "Access").
func CheckContext(ctx context.Context, object, action string) bool {
	user := ctx.User()
	if user == nil {
		return false
	}
	return rbac.Check(ctx, ctx.Subject(), object, action) && TokenAllowsObject(ctx, ctx.Subject(), object, action)
}

// HasPermission is rbac.HasPermission, limited by the request's access token when the subject is the request's own
// user. Checks made as another subject, e.g. a playbook during its run, aren't limited.
func HasPermission(ctx context.Context, subject string, attr *models.ABACAttribute, action string) bool {
	return rbac.HasPermission(ctx, subject, attr, action) && TokenAllows(ctx, subject, attr, action)
}

// TokenAllowsObject reports whether the RoleBindings that select the request's access token allow a check as the subject on a whole
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

// TokenAllows reports whether the RoleBindings that select the request's access token allow a check as the subject on resources.
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
