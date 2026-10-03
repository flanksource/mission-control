package adapter

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// Permission expansion error codes
const (
	ErrScopeExpansionInvalidObjectSelector = "ErrScopeExpansionInvalidObjectSelector"
	ErrScopeExpansionScopeNotFound         = "ErrScopeExpansionScopeNotFound"
	ErrScopeExpansionInvalidScopeTargets   = "ErrScopeExpansionInvalidScopeTargets"
)

// Reasons an object isn't in effect. They're stored in the error_reason column of scopes, roles and role_bindings,
// and reported as the reason of the Ready condition.
const (
	ReasonInvalid                  = "Invalid"
	ReasonAgentNotFound            = "AgentNotFound"
	ReasonScopeNotFound            = "ScopeNotFound"
	ReasonScopeInvalid             = "ScopeInvalid"
	ReasonRoleNotFound             = "RoleNotFound"
	ReasonRoleInvalid              = "RoleInvalid"
	ReasonRowLevelSecurityRequired = "RowLevelSecurityRequired"

	// ReasonNoRulesApply is a binding with a constraint that none of its role's allow rules applies through.
	ReasonNoRulesApply = "NoRulesApply"

	// ReasonConstraintDoesNotFit is an allow rule a binding's constraint can't narrow.
	ReasonConstraintDoesNotFit = "ConstraintDoesNotFit"
)

// scopeExpansionValidationError represents a validation failure that should be persisted
type scopeExpansionValidationError struct {
	reason  string
	message string
}

func (e *scopeExpansionValidationError) Error() string {
	return e.message
}

func NewValidationError(format string, args ...any) error {
	return NewInvalid(ReasonInvalid, format, args...)
}

// NewInvalid returns a validation error with a reason.
func NewInvalid(reason, format string, args ...any) error {
	return &scopeExpansionValidationError{reason: reason, message: fmt.Sprintf(format, args...)}
}

// IsValidationError reports whether the error is a problem with the referenced scopes,
// such as a missing scope, rather than a failure to look them up.
func IsValidationError(err error) bool {
	var validationErr *scopeExpansionValidationError
	return errors.As(err, &validationErr)
}

// InvalidReason returns the reason of a validation error, or "" for any other error.
func InvalidReason(err error) string {
	var validationErr *scopeExpansionValidationError
	if errors.As(err, &validationErr) {
		return validationErr.reason
	}
	return ""
}

// withReason returns a validation error with a different reason and the same message.
func withReason(err error, reason string) error {
	if IsValidationError(err) {
		return NewInvalid(reason, "%v", err)
	}
	return err
}

// cachedScope is a valid stored Scope, with its agents resolved to ids.
type cachedScope struct {
	id      string
	targets []v1.ScopeTarget
}

// getScope retrieves a scope by namespace and name, using the cache when given.
// Returns nil if the scope doesn't exist, and a validation error if it's invalid: an invalid Scope selects nothing.
func getScope(ctx context.Context, cache *gocache.Cache, namespace, name string) (*cachedScope, error) {
	cacheKey := namespace + "/" + name

	if cache != nil {
		if cached, found := cache.Get(cacheKey); found {
			return cached.(*cachedScope), nil
		}
	}

	var scope models.Scope
	err := ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).Find(&scope).Error
	if err != nil {
		return nil, err
	} else if scope.ID == uuid.Nil {
		return nil, nil
	}

	var targets []v1.ScopeTarget
	if err := json.Unmarshal([]byte(scope.Targets), &targets); err != nil {
		return nil, NewInvalid(ReasonScopeInvalid, "%s:%s/%s", ErrScopeExpansionInvalidScopeTargets, namespace, name)
	}

	resolved, err := ValidateScope(ctx, cache, targets)
	if err != nil {
		return nil, withContext(withReason(err, ReasonScopeInvalid), "scope %s/%s is invalid", namespace, name)
	}

	result := &cachedScope{id: scope.ID.String(), targets: resolved}
	if cache != nil {
		cache.Set(cacheKey, result, gocache.DefaultExpiration)
	}

	return result, nil
}

// LoadScope returns the id and targets of a valid Scope, with its agents resolved to ids.
// It returns nil targets for a missing Scope, and a validation error for an invalid one.
func LoadScope(ctx context.Context, cache *gocache.Cache, namespace, name string) (string, []v1.ScopeTarget, error) {
	scope, err := getScope(ctx, cache, namespace, name)
	if err != nil || scope == nil {
		return "", nil, err
	}
	return scope.id, scope.targets, nil
}

// ExpandPermissionScopes expands scope references in a permission's object_selector
// and returns a new permission with the expanded selectors merged in.
// This is the exported version for testing.
func ExpandPermissionScopes(ctx context.Context, cache *gocache.Cache, perm models.Permission) ([]v1.PermissionObject, error) {
	// If no object selector, nothing to expand
	if len(perm.ObjectSelector) == 0 {
		return nil, nil
	}

	var selectors v1.PermissionObject
	if err := json.Unmarshal(perm.ObjectSelector, &selectors); err != nil {
		return nil, NewValidationError(ErrScopeExpansionInvalidObjectSelector)
	}

	return expandObjectScopes(ctx, cache, selectors)
}

// expandObjectScopes returns one object per target of the scopes the object references,
// or nil when it references no scopes.
func expandObjectScopes(ctx context.Context, cache *gocache.Cache, selectors v1.PermissionObject) ([]v1.PermissionObject, error) {
	if len(selectors.Scopes) == 0 {
		return nil, nil
	}

	var output []v1.PermissionObject

	// Expand scopes and merge into selectors
	for _, scopeRef := range selectors.Scopes {
		scope, err := getScope(ctx, cache, scopeRef.Namespace, scopeRef.Name)
		if err != nil {
			if IsValidationError(err) {
				return nil, err
			}

			return nil, fmt.Errorf("failed to get scope targets: %w", err)
		} else if scope == nil {
			return nil, NewInvalid(ReasonScopeNotFound, "%s:%s/%s", ErrScopeExpansionScopeNotFound, scopeRef.Namespace, scopeRef.Name)
		}

		// Merge targets into selectors (union approach)
		for _, target := range scope.targets {
			var selectors v1.PermissionObject
			_, selector := target.Selector()
			if target.Config != nil {
				selectors.Configs = append(selectors.Configs, selector)
			}
			if target.Component != nil {
				selectors.Components = append(selectors.Components, selector)
			}
			if target.Playbook != nil {
				selectors.Playbooks = append(selectors.Playbooks, selector)
			}
			if target.Connection != nil {
				selectors.Connections = append(selectors.Connections, selector)
			}
			if target.View != nil {
				selectors.Views = append(selectors.Views, dutyRBAC.ViewRef{
					ID:        target.View.ID,
					Name:      target.View.Name,
					Namespace: target.View.Namespace,
				})
			}

			// Canary and check targets have no selectors here, and an object without selectors would match everything
			if selectors.HasSelectors() {
				output = append(output, selectors)
			}
		}
	}

	if len(output) == 0 {
		return nil, NewValidationError("%s: scopes select no resources a permission can match", ErrScopeExpansionInvalidScopeTargets)
	}

	return output, nil
}
