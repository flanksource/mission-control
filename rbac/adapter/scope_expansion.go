package adapter

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

const ErrScopeExpansionInvalidScopeTargets = "ErrScopeExpansionInvalidScopeTargets"

// Reasons an object isn't in effect. They're stored in the error_reason column of scopes, roles and role_bindings,
// and reported as the reason of the Ready condition.
const (
	ReasonInvalid       = "Invalid"
	ReasonAgentNotFound = "AgentNotFound"
	ReasonScopeNotFound = "ScopeNotFound"
	ReasonScopeInvalid  = "ScopeInvalid"
	ReasonRoleNotFound  = "RoleNotFound"
	ReasonRoleInvalid   = "RoleInvalid"

	// ReasonNoRulesApply is a binding that none of its role's rules applies through.
	ReasonNoRulesApply = "NoRulesApply"

	// ReasonConstraintDoesNotFit is a rule a binding's constraint can't narrow.
	ReasonConstraintDoesNotFit = "ConstraintDoesNotFit"

	// ReasonDenyOnRoleBindings is a rule of a deny binding that only selects RoleBindings, which are never denied.
	ReasonDenyOnRoleBindings = "DenyOnRoleBindings"

	// ReasonPluginRuleNotAllowed is a binding that selects a plugin and binds a role with a rule a plugin can't be granted.
	ReasonPluginRuleNotAllowed = "PluginRuleNotAllowed"
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
	shipped bool
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

	resolved, err := ValidateScope(ctx, targets)
	if err != nil {
		return nil, withContext(withReason(err, ReasonScopeInvalid), "scope %s/%s is invalid", namespace, name)
	}

	result := &cachedScope{id: scope.ID.String(), shipped: IsShipped(scope.Namespace, scope.Source), targets: resolved}
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
