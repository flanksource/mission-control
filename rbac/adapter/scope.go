package adapter

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// ScopeSelection is what a Scope selects, by resource type.
type ScopeSelection struct {
	ID        string
	Name      string
	Selectors map[string][]types.ResourceSelector
}

// DeclaredTypes returns the resource types the Scope's targets select.
func (s ScopeSelection) DeclaredTypes() []string {
	kinds := lo.Keys(s.Selectors)
	slices.Sort(kinds)
	return kinds
}

// ResolveScope returns what the named Scope, in the namespace, selects.
// A missing or invalid Scope is a validation error.
func ResolveScope(ctx context.Context, cache *gocache.Cache, namespace, name string) (ScopeSelection, error) {
	scope, err := getScope(ctx, cache, namespace, name)
	if err != nil {
		if IsValidationError(err) {
			return ScopeSelection{}, err
		}
		return ScopeSelection{}, fmt.Errorf("failed to get scope %s/%s: %w", namespace, name, err)
	} else if scope == nil {
		return ScopeSelection{}, NewInvalid(ReasonScopeNotFound, "scope %s/%s not found", namespace, name)
	}

	return NewScopeSelection(scope.id, name, scope.targets), nil
}

// NewScopeSelection returns what the targets of a valid Scope select.
func NewScopeSelection(id, name string, targets []v1.ScopeTarget) ScopeSelection {
	selection := ScopeSelection{ID: id, Name: name, Selectors: map[string][]types.ResourceSelector{}}
	for _, target := range targets {
		kind, selector := target.Selector()
		selection.Selectors[kind] = append(selection.Selectors[kind], selector)
	}

	return selection
}

// ValidateScope validates the targets of a Scope, and returns them with every agent resolved to its id.
// Agents are resolved on every validation: a name follows the agent registered under it,
// an id only matches that one registration.
func ValidateScope(ctx context.Context, targets []v1.ScopeTarget) ([]v1.ScopeTarget, error) {
	if err := ValidateScopeTargets(targets); err != nil {
		return nil, NewValidationError("%v", err)
	}

	resolved := make([]v1.ScopeTarget, 0, len(targets))
	for i, target := range targets {
		target = *target.DeepCopy()
		kind, selector := target.Selector()
		if selector.Agent != "" {
			id, err := resolveAgent(ctx, selector.Agent)
			if err != nil {
				return nil, withContext(err, "target %d (%s)", i, kind)
			}
			switch {
			case target.Config != nil:
				target.Config.Agent = id
			case target.Component != nil:
				target.Component.Agent = id
			case target.Check != nil:
				target.Check.Agent = id
			case target.Canary != nil:
				target.Canary.Agent = id
			}
		}
		resolved = append(resolved, target)
	}

	return resolved, nil
}

// resolveAgent returns the id of the agent with the given name or id.
func resolveAgent(ctx context.Context, agent string) (string, error) {
	query := ctx.DB().Model(&models.Agent{}).Select("id").Where("deleted_at IS NULL")
	if uuid.Validate(agent) == nil {
		query = query.Where("id = ?", agent)
	} else {
		query = query.Where("name = ?", agent)
	}

	var ids []uuid.UUID
	if err := query.Limit(1).Find(&ids).Error; err != nil {
		return "", fmt.Errorf("failed to resolve agent %q: %w", agent, err)
	} else if len(ids) == 0 {
		return "", NewInvalid(ReasonAgentNotFound, "agent %q not found", agent)
	}

	return ids[0].String(), nil
}

// maxScopeTargets is the most targets a Scope can have.
const maxScopeTargets = 10

// ValidateScopeTargets validates the targets of a Scope on their own: each selects exactly one resource type,
// with a selector that type supports. A Scope that fails it can never become valid, so the API rejects it.
func ValidateScopeTargets(targets []v1.ScopeTarget) error {
	if len(targets) == 0 {
		return fmt.Errorf("a scope must have at least one target")
	} else if len(targets) > maxScopeTargets {
		return fmt.Errorf("a scope can have at most %d targets, not %d", maxScopeTargets, len(targets))
	}

	for i, target := range targets {
		if n := lo.Count([]bool{
			target.Config != nil, target.Component != nil, target.Check != nil, target.Canary != nil,
			target.Playbook != nil, target.View != nil, target.Connection != nil,
		}, true); n != 1 {
			return fmt.Errorf("target %d must select exactly one resource type, not %d", i, n)
		}

		kind, selector := target.Selector()
		if err := ValidateSelector(kind, selector); err != nil {
			return fmt.Errorf("target %d (%s): %w", i, kind, err)
		}
	}

	return nil
}

// ValidateSelector checks the values of a converted Scope selector.
// name matches exactly, any name when set to "*", or names starting with a prefix when it ends with "*".
// namespace and id only match exactly. Other patterns, lists and exclusions aren't supported.
func ValidateSelector(kind string, selector types.ResourceSelector) error {
	if selector.ID == "" && selector.Name == "" && selector.Namespace == "" && selector.Agent == "" &&
		len(selector.Types) == 0 && selector.TagSelector == "" && selector.LabelSelector == "" {
		return fmt.Errorf(`an empty selector selects nothing; use name: "*" to select every %s`, kind)
	}

	if err := ValidateName(selector.Name); err != nil {
		return err
	}
	if err := ValidateExact("namespace", selector.Namespace); err != nil {
		return err
	}

	if selector.ID != "" {
		if id, err := uuid.Parse(selector.ID); err != nil || id.String() != selector.ID {
			return fmt.Errorf("id %q must be a lowercase UUID", selector.ID)
		}
	}

	for _, value := range selector.Types {
		if value == "" || strings.ContainsAny(value, "*!") {
			return fmt.Errorf("types value %q must be exact and non-empty", value)
		}
	}

	for field, value := range map[string]string{"tagSelector": selector.TagSelector, "labelSelector": selector.LabelSelector} {
		if value == "" {
			continue
		}
		if err := validateEqualities(field, value); err != nil {
			return err
		}
	}

	return nil
}

// validateEqualities checks a tag or label selector: one or more key=value pairs joined by commas.
// Every other operator of the label selector syntax is rejected.
func validateEqualities(field, value string) error {
	parsed, err := labels.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", field, value, err)
	}

	requirements, _ := parsed.Requirements()
	for _, r := range requirements {
		var operator string
		switch r.Operator() {
		case selection.Equals:
			continue
		case selection.Exists:
			operator = "a bare key"
		case selection.DoesNotExist:
			operator = "!key"
		default:
			operator = fmt.Sprintf("%q", string(r.Operator()))
		}
		return fmt.Errorf("%s %q uses %s: only key=value pairs are supported", field, value, operator)
	}

	return nil
}

// ValidateName checks a name: an exact value, "*" for any name, or a prefix followed by one "*".
// Lists and exclusions aren't supported.
func ValidateName(value string) error {
	if value == "*" {
		return nil
	}
	if prefix, ok := strings.CutSuffix(value, "*"); ok && prefix != "" && !strings.ContainsAny(prefix, "*!,") {
		return nil
	}
	if strings.ContainsAny(value, "*!,") {
		return fmt.Errorf(`name %q must be an exact value, "*", or a prefix followed by "*"; other patterns, lists and exclusions aren't supported`, value)
	}
	return nil
}

// ValidateExact checks a value that only matches exactly. To match any value, the field is omitted.
func ValidateExact(field, value string) error {
	if strings.ContainsAny(value, "*!,") {
		return fmt.Errorf(`%s %q must be an exact value; omit %s to match any, patterns, lists and exclusions aren't supported`, field, value, field)
	}
	return nil
}
