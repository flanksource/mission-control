package adapter

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/types"
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
func getScopeTargets(ctx context.Context, cache *gocache.Cache, namespace, name string) ([]v1.ScopeTarget, error) {
	cacheKey := namespace + "/" + name

	if cached, found := cache.Get(cacheKey); found {
		return cached.([]v1.ScopeTarget), nil
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
		return nil, NewValidationError("%s:%s/%s", ErrScopeExpansionInvalidScopeTargets, namespace, name)
	}

	cache.Set(cacheKey, targets, gocache.DefaultExpiration)

	return targets, nil
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

	// If no scope references, return as-is
	if len(selectors.Scopes) == 0 {
		return nil, nil
	}

	var output []v1.PermissionObject

	// Expand scopes and merge into selectors
	for _, scopeRef := range selectors.Scopes {
		targets, err := getScopeTargets(ctx, cache, scopeRef.Namespace, scopeRef.Name)
		if err != nil {
			var validationErr *scopeExpansionValidationError
			if errors.As(err, &validationErr) {
				return nil, err
			}

			return nil, fmt.Errorf("failed to get scope targets: %w", err)
		} else if targets == nil {
			return nil, NewValidationError("%s:%s/%s", ErrScopeExpansionScopeNotFound, scopeRef.Namespace, scopeRef.Name)
		}

		// Merge targets into selectors (union approach)
		for _, target := range targets {
			var selectors v1.PermissionObject
			if target.Config != nil {
				selectors.Configs = append(selectors.Configs, convertScopeResourceSelectorToResourceSelector(target.Config))
			}
			if target.Component != nil {
				selectors.Components = append(selectors.Components, convertScopeResourceSelectorToResourceSelector(target.Component))
			}
			if target.Playbook != nil {
				selectors.Playbooks = append(selectors.Playbooks, convertScopeResourceSelectorToResourceSelector(target.Playbook))
			}
			if target.View != nil {
				selectors.Views = append(selectors.Views, dutyRBAC.ViewRef{
					Name:      target.View.Name,
					Namespace: target.View.Namespace,
				})
			}

			output = append(output, selectors)

			// Note: Canary targets are skipped - v1.PermissionObject doesn't have a Canaries field
			// Note: Global targets are ignored per design decision
			// Note: Connection targets are ignored (no RLS support yet per auth/rls.go:127-132)
		}
	}

	return output, nil
}

// convertScopeResourceSelectorToResourceSelector converts a v1.ScopeResourceSelector to types.ResourceSelector
func convertScopeResourceSelectorToResourceSelector(scopeSel *v1.ScopeResourceSelector) types.ResourceSelector {
	return types.ResourceSelector{
		Agent:       scopeSel.Agent,
		Name:        scopeSel.Name,
		Namespace:   scopeSel.Namespace,
		TagSelector: scopeSel.TagSelector,
	}
}
