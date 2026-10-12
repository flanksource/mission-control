package rbac

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// tokenLimits caches the Roles that limit each subject's access token, by subject. A token's Roles never change,
// and a deleted token can't authenticate, so entries only expire to bound the cache.
var tokenLimits = gocache.New(time.Hour, time.Hour)

// TokenLimit returns the Roles that limit the subject, and whether it's limited.
// A subject is limited when it's the person of an access token that lists Roles. A token that lists none,
// e.g. an agent's, isn't limited. When the token can't be read, the subject is limited by no Role, so it may do nothing.
func TokenLimit(ctx context.Context, subject string) (models.AccessTokenRoles, bool) {
	if uuid.Validate(subject) != nil {
		return nil, false
	}

	if cached, ok := tokenLimits.Get(subject); ok {
		roles := cached.(models.AccessTokenRoles)
		return roles, roles != nil
	}

	var tokens []models.AccessToken
	if err := ctx.DB().Select("id", "roles").Where("person_id = ?", subject).
		Where("roles IS NOT NULL").
		Find(&tokens).Error; err != nil {
		ctx.Errorf("failed to read the roles of the access token of %s: %v", subject, err)
		return nil, true
	}

	var roles models.AccessTokenRoles
	for _, token := range tokens {
		roles = append(roles, token.Roles...)
	}
	if len(tokens) > 0 && roles == nil {
		roles = models.AccessTokenRoles{}
	}

	tokenLimits.SetDefault(subject, roles)
	return roles, roles != nil
}

// withinTokenLimit reports whether the subject's access token allows a check: it isn't limited,
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

// CheckContext is rbac.CheckContext for a check on a whole object, limited by the Roles of the request's access token.
func CheckContext(ctx context.Context, object, action string) bool {
	return rbac.CheckContext(ctx, object, action) && TokenAllowsObject(ctx, ctx.User().ID.String(), object, action)
}

// HasPermission is rbac.HasPermission, limited by the Roles of the subject's access token when it's the person
// of one. Checks made as another subject, e.g. a playbook during its run, aren't limited.
func HasPermission(ctx context.Context, subject string, attr *models.ABACAttribute, action string) bool {
	return rbac.HasPermission(ctx, subject, attr, action) && TokenAllows(ctx, subject, attr, action)
}

// TokenAllowsObject reports whether the Roles of the subject's access token allow a check on a whole object.
// It's true when the subject isn't limited by a token.
func TokenAllowsObject(ctx context.Context, subject, object, action string) bool {
	return withinTokenLimit(ctx, subject, func(principal string) bool {
		allowed, err := rbac.Enforcer().Enforce(principal, object, action)
		if err != nil {
			ctx.Errorf("failed to check %s:%s against %s: %v", object, action, principal, err)
		}
		return err == nil && allowed
	})
}

// TokenAllows reports whether the Roles of the subject's access token allow a check on resources.
// It's true when the subject isn't limited by a token.
func TokenAllows(ctx context.Context, subject string, attr *models.ABACAttribute, action string) bool {
	return withinTokenLimit(ctx, subject, func(principal string) bool {
		return rbac.HasPermission(ctx, principal, attr, action)
	})
}
