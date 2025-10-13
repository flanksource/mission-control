package adapter

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/types"
	gocache "github.com/patrickmn/go-cache"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// Permission expansion error codes
const (
	ErrScopeExpansionInvalidObjectSelector = "ErrScopeExpansionInvalidObjectSelector"
	ErrScopeExpansionScopeNotFound         = "ErrScopeExpansionScopeNotFound"
	ErrScopeExpansionInvalidScopeTargets   = "ErrScopeExpansionInvalidScopeTargets"
)

// scopeExpansionValidationError represents a permission validation failure that should be persisted
type scopeExpansionValidationError struct {
	message string
}

func (e *scopeExpansionValidationError) Error() string {
	return e.message
}

func NewValidationError(format string, args ...any) error {
	return &scopeExpansionValidationError{message: fmt.Sprintf(format, args...)}
}

// getScopeTargets retrieves scope targets by namespace and name, using cache when available.
// Returns gorm.ErrRecordNotFound if scope doesn't exist.
// Returns json unmarshal error if scope.Targets contains invalid JSON.
func (a *PermissionAdapter) getScopeTargets(namespace, name string) ([]v1.ScopeTarget, error) {
	cacheKey := namespace + "/" + name

	if cached, found := a.cache.Get(cacheKey); found {
		return cached.([]v1.ScopeTarget), nil
	}

	var scope models.Scope
	err := a.db.
		Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).
		First(&scope).Error
	if err != nil {
		return nil, err
	}

	var targets []v1.ScopeTarget
	if err := json.Unmarshal([]byte(scope.Targets), &targets); err != nil {
		return nil, NewValidationError("%s:%s/%s", ErrScopeExpansionInvalidScopeTargets, namespace, name)
	}

	a.cache.Set(cacheKey, targets, gocache.DefaultExpiration)

	return targets, nil
}

// expandPermissionScopes expands scope references in a permission's object_selector
// and returns a new permission with the expanded selectors merged in.
func (a *PermissionAdapter) expandPermissionScopes(perm models.Permission) (models.Permission, error) {
	// If no object selector, nothing to expand
	if len(perm.ObjectSelector) == 0 {
		return perm, nil
	}

	var selectors dutyRBAC.Selectors
	if err := json.Unmarshal(perm.ObjectSelector, &selectors); err != nil {
		return perm, NewValidationError(ErrScopeExpansionInvalidObjectSelector)
	}

	// If no scope references, return as-is
	if len(selectors.Scopes) == 0 {
		return perm, nil
	}

	// Expand scopes and merge into selectors
	for _, scopeRef := range selectors.Scopes {
		targets, err := a.getScopeTargets(scopeRef.Namespace, scopeRef.Name)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return perm, NewValidationError("%s:%s/%s", ErrScopeExpansionScopeNotFound, scopeRef.Namespace, scopeRef.Name)
			}

			var validationErr *scopeExpansionValidationError
			if errors.As(err, &validationErr) {
				return perm, err
			}

			return perm, fmt.Errorf("failed to get scope targets: %w", err)
		}

		// Merge targets into selectors (union approach)
		for _, target := range targets {
			if target.Config != nil {
				selectors.Configs = append(selectors.Configs, convertScopeResourceSelectorToResourceSelector(target.Config))
			}
			if target.Component != nil {
				selectors.Components = append(selectors.Components, convertScopeResourceSelectorToResourceSelector(target.Component))
			}
			if target.Playbook != nil {
				selectors.Playbooks = append(selectors.Playbooks, convertScopeResourceSelectorToResourceSelector(target.Playbook))
			}

			// Note: Canary targets are skipped - dutyRBAC.Selectors doesn't have a Canaries field
			// Note: Global targets are ignored per design decision
			// Note: Connection targets are ignored (no RLS support yet per auth/rls.go:127-132)
		}
	}

	// Marshal the expanded selectors back to JSON
	expandedObjectSelector, err := json.Marshal(selectors)
	if err != nil {
		return perm, fmt.Errorf("failed to marshal expanded selectors: %w", err)
	}

	// Create a new permission with the expanded object selector
	expandedPerm := perm
	expandedPerm.ObjectSelector = expandedObjectSelector

	return expandedPerm, nil
}

// convertScopeResourceSelectorToResourceSelector converts a v1.ScopeResourceSelector to types.ResourceSelector
func convertScopeResourceSelectorToResourceSelector(scopeSel *v1.ScopeResourceSelector) types.ResourceSelector {
	return types.ResourceSelector{
		Agent:       scopeSel.Agent,
		Name:        scopeSel.Name,
		TagSelector: scopeSel.TagSelector,
	}
}
