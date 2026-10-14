package adapter

import (
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

func init() {
	dutyRBAC.ActionContract = func(action string) ([]string, []string, bool) {
		contract, err := ContractFor(action)
		if err != nil {
			return nil, nil, false
		}
		return contract.Resources, contract.Targets, true
	}
}

// MembershipTargets converts a valid Scope's targets and exclusions, with agents resolved, to the targets its
// membership is built from. Targets of types whose membership isn't stored, i.e. views, are left out.
func MembershipTargets(spec v1.ScopeSpec) []membership.Target {
	var out []membership.Target
	for _, targets := range []struct {
		targets []v1.ScopeTarget
		exclude bool
	}{{spec.Targets, false}, {spec.Exclude, true}} {
		for _, target := range targets.targets {
			kind, selector := target.Selector()
			if membership.Supported(kind) {
				out = append(out, membership.Target{Type: kind, Selector: selector, Exclude: targets.exclude})
			}
		}
	}
	return out
}

// SaveScopeMembership brings a Scope's membership in line with the version just saved, in the context's transaction:
// a valid Scope's targets and members are rebuilt from its resolved targets and exclusions; an invalid one's are cleared.
func SaveScopeMembership(ctx context.Context, scope models.Scope, resolved v1.ScopeSpec, validationErr error) error {
	if validationErr != nil {
		return membership.Clear(ctx, scope.ID)
	}

	if _, err := membership.Rebuild(ctx, scope, MembershipTargets(resolved)); err != nil {
		return fmt.Errorf("failed to save the membership of scope %s/%s: %w", scope.Namespace, scope.Name, err)
	}
	return nil
}

// SyncScopeMembership validates a stored Scope and brings its membership in line with it: a valid Scope's targets
// and members are rebuilt when its targets or exclusions, or the agents they resolve to, changed; an invalid or deleted one's are
// cleared. An invalid Scope's validation error is returned once its membership is cleared (see IsValidationError).
// A Scope saved again since it was loaded is left to that save.
func SyncScopeMembership(ctx context.Context, scope models.Scope) error {
	if scope.DeletedAt != nil {
		return membership.Clear(ctx, scope.ID)
	}

	spec, err := ScopeSpec(scope)
	if err != nil {
		validationErr := NewValidationError("%v", err)
		if err := membership.Clear(ctx, scope.ID); err != nil {
			return err
		}
		return validationErr
	}

	resolved, validationErr := ValidateScope(ctx, spec)
	if validationErr != nil && !IsValidationError(validationErr) {
		return validationErr
	}

	if err := SaveScopeMembership(ctx, scope, resolved, validationErr); errors.Is(err, membership.ErrStaleScope) {
		return validationErr
	} else if err != nil {
		return err
	}
	return validationErr
}

// BuildScopeMemberships builds every Scope that has neither targets nor members, and clears the membership of invalid
// ones. Mission Control runs it before accepting requests: a grant through a Scope with no membership rows is refused,
// so serving earlier would refuse grants that are in effect.
func BuildScopeMemberships(ctx context.Context) error {
	var scopes []models.Scope
	if err := ctx.DB().Raw(`SELECT * FROM scopes s
		WHERE s.deleted_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM scope_targets t WHERE t.scope_id = s.id)
			AND NOT EXISTS (SELECT 1 FROM scope_members m WHERE m.scope_id = s.id)`).Scan(&scopes).Error; err != nil {
		return fmt.Errorf("failed to load scopes without membership: %w", err)
	}

	for _, scope := range scopes {
		if err := SyncScopeMembership(ctx, scope); err != nil && !IsValidationError(err) {
			return err
		}
	}
	return nil
}
