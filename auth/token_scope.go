package auth

import (
	"slices"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// tokenScopeActionsProperty lists the actions without a resource that scoped access tokens already deny unless listed.
const tokenScopeActionsProperty = "rbac.token_scope.resourceless_actions"

// LimitScopedTokens makes scoped access tokens deny the actions without a resource added since they were created.
//
// A scoped token denies every object and action its scope doesn't list, but only those known when it was created.
// An action added later would otherwise reach the token through its owner, whatever its scope.
func LimitScopedTokens(ctx context.Context) error {
	var recorded string
	if err := ctx.DB().Raw("SELECT value FROM properties WHERE name = ?", tokenScopeActionsProperty).Scan(&recorded).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to read %s", tokenScopeActionsProperty)
	}
	known := strings.Split(recorded, ",")

	permissions := adapter.ResourcelessPermissions()
	var missing [][]string
	for _, permission := range permissions {
		if !slices.Contains(known, permission.Action) {
			permission.Deny = true
			missing = append(missing, permission.ToArgsWithoutSubject())
		}
	}
	if len(missing) == 0 {
		return nil
	}

	tokens, err := scopedTokenSubjects(ctx)
	if err != nil {
		return err
	}

	for _, token := range tokens {
		for _, rule := range missing {
			if _, err := rbac.Enforcer().AddPermissionsForUser(token, rule); err != nil {
				return ctx.Oops().Wrapf(err, "failed to limit access token %s", token)
			}
		}
	}

	if len(tokens) > 0 {
		if err := rbac.Enforcer().InvalidateCache(); err != nil {
			return ctx.Oops().Wrapf(err, "failed to invalidate the rbac cache")
		}
	}

	actions := lo.Map(permissions, func(p policy.Permission, _ int) string { return p.Action })
	return context.UpdateProperty(ctx, tokenScopeActionsProperty, strings.Join(actions, ","))
}

// scopedTokenSubjects returns the subjects of the access tokens created with a scope, which have token-scope denies.
func scopedTokenSubjects(ctx context.Context) ([]string, error) {
	denies, err := rbac.Enforcer().GetFilteredPolicy(3, "deny")
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	subjects := lo.Uniq(lo.FilterMap(denies, func(rule []string, _ int) (string, bool) {
		return rule[0], len(rule) > 5 && rule[5] == "na"
	}))
	if len(subjects) == 0 {
		return nil, nil
	}

	var tokens []string
	if err := ctx.DB().Model(&models.AccessToken{}).Where("person_id::text IN ?", subjects).
		Pluck("person_id::text", &tokens).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list scoped access tokens")
	}

	return tokens, nil
}
