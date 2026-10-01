package adapter

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/labels"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// resourceGlobal is the legacy Scope target that selects resources of every type. Only Permissions use it.
const resourceGlobal = "global"

// supportedFields lists the selector fields each resource type supports.
var supportedFields = map[string][]string{
	policy.ResourceConfig:     {"id", "name", "namespace", "agent", "types", "statuses", "health", "tagSelector", "labelSelector", "fieldSelector"},
	policy.ResourceComponent:  {"id", "name", "namespace", "agent", "types", "statuses", "health", "labelSelector", "fieldSelector"},
	policy.ResourceCheck:      {"id", "name", "namespace", "agent", "types", "statuses", "health", "labelSelector", "fieldSelector"},
	policy.ResourceCanary:     {"id", "name", "namespace", "agent", "labelSelector"},
	policy.ResourcePlaybook:   {"id", "name", "namespace", "fieldSelector"},
	policy.ResourceView:       {"id", "name", "namespace"},
	policy.ResourceConnection: {"id", "name", "namespace", "types"},
	resourceGlobal:            {"id", "name", "namespace", "agent", "tagSelector"},
}

// playbookSelectableFields are the fields a playbook fieldSelector can match.
var playbookSelectableFields = []string{"category"}

// ScopeSelection is what a Scope selects, by resource type.
type ScopeSelection struct {
	ID        string
	Name      string
	Selectors map[string][]types.ResourceSelector

	// Global is set when the Scope has a global target, which only Permissions can use.
	Global bool
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
		kind, selector := targetSelector(target)
		if kind == resourceGlobal {
			selection.Global = true
			continue
		}
		selection.Selectors[kind] = append(selection.Selectors[kind], *selector)
	}

	return selection
}

// ValidateScope validates the targets of a Scope, and returns them with every agent resolved to its id.
// Agents are resolved on every validation: a name follows the agent registered under it,
// an id only matches that one registration.
func ValidateScope(ctx context.Context, cache *gocache.Cache, targets []v1.ScopeTarget) ([]v1.ScopeTarget, error) {
	if err := ValidateScopeTargets(targets); err != nil {
		return nil, NewValidationError("%v", err)
	}

	resolved := make([]v1.ScopeTarget, 0, len(targets))
	for i, target := range targets {
		target = *target.DeepCopy()
		kind, selector := targetSelector(target)
		if selector.Agent != "" {
			id, err := resolveAgent(ctx, cache, selector.Agent)
			if err != nil {
				return nil, withContext(err, "target %d (%s)", i, kind)
			}
			selector.Agent = id
		}
		resolved = append(resolved, target)
	}

	return resolved, nil
}

// resolveAgent returns the id of the agent with the given name or id.
func resolveAgent(ctx context.Context, cache *gocache.Cache, agent string) (string, error) {
	cacheKey := "agent:" + agent
	if cache != nil {
		if cached, found := cache.Get(cacheKey); found {
			return cached.(string), nil
		}
	}

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

	id := ids[0].String()
	if cache != nil {
		cache.Set(cacheKey, id, gocache.DefaultExpiration)
	}
	return id, nil
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
		if n := len(lo.Compact([]*types.ResourceSelector{
			target.Config, target.Component, target.Check, target.Canary, target.Playbook,
			target.View, target.Connection, target.Global,
		})); n != 1 {
			return fmt.Errorf("target %d must select exactly one resource type, not %d", i, n)
		}

		kind, selector := targetSelector(target)
		if err := ValidateSelector(kind, *selector); err != nil {
			return fmt.Errorf("target %d (%s): %w", i, kind, err)
		}
	}

	return nil
}

func targetSelector(target v1.ScopeTarget) (string, *types.ResourceSelector) {
	switch {
	case target.Config != nil:
		return policy.ResourceConfig, target.Config
	case target.Component != nil:
		return policy.ResourceComponent, target.Component
	case target.Check != nil:
		return policy.ResourceCheck, target.Check
	case target.Canary != nil:
		return policy.ResourceCanary, target.Canary
	case target.Playbook != nil:
		return policy.ResourcePlaybook, target.Playbook
	case target.View != nil:
		return policy.ResourceView, target.View
	case target.Connection != nil:
		return policy.ResourceConnection, target.Connection
	case target.Global != nil:
		return resourceGlobal, target.Global
	}
	return "", nil
}

// ValidateSelector checks that a selector only uses fields its resource type supports, with valid values.
// name matches exactly, or any name when set to "*". namespace and id only match exactly.
// Patterns, lists and exclusions aren't supported.
func ValidateSelector(kind string, selector types.ResourceSelector) error {
	supported, ok := supportedFields[kind]
	if !ok {
		return fmt.Errorf("unknown resource type %q", kind)
	}

	fields := selectorFields(selector)
	if len(fields) == 0 {
		return fmt.Errorf(`an empty selector selects nothing; use name: "*" to select every %s`, kind)
	}

	for _, field := range fields {
		if !slices.Contains(supported, field) {
			return fmt.Errorf("%s selectors don't support %s", kind, field)
		}
	}

	if err := ValidateExactOrAny("name", selector.Name); err != nil {
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

	for field, values := range map[string][]string{
		"types":    selector.Types,
		"statuses": selector.Statuses,
		"health":   strings.Split(string(selector.Health), ","),
	} {
		for _, value := range values {
			if strings.ContainsAny(value, "*!") {
				return fmt.Errorf("%s value %q must be exact", field, value)
			}
		}
	}

	for field, value := range map[string]string{"tagSelector": selector.TagSelector, "labelSelector": selector.LabelSelector} {
		if value == "" {
			continue
		}
		if _, err := labels.Parse(value); err != nil {
			return fmt.Errorf("invalid %s %q: %w", field, value, err)
		}
	}

	if selector.FieldSelector != "" {
		parsed, err := labels.Parse(selector.FieldSelector)
		if err != nil {
			return fmt.Errorf("invalid fieldSelector %q: %w", selector.FieldSelector, err)
		}

		if kind == policy.ResourcePlaybook {
			requirements, _ := parsed.Requirements()
			for _, requirement := range requirements {
				if !slices.Contains(playbookSelectableFields, requirement.Key()) {
					return fmt.Errorf("playbook fieldSelector can only match %s, not %q", strings.Join(playbookSelectableFields, ", "), requirement.Key())
				}
			}
		}
	}

	return nil
}

// ValidateExactOrAny checks a value that matches exactly, or matches any value when it's "*".
func ValidateExactOrAny(field, value string) error {
	if value == "*" {
		return nil
	}
	if strings.ContainsAny(value, "*!,") {
		return fmt.Errorf(`%s %q must be an exact value, or "*"; patterns, lists and exclusions aren't supported`, field, value)
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

// selectorFields returns the fields a selector sets, including query options that aren't conditions.
func selectorFields(selector types.ResourceSelector) []string {
	var fields []string
	add := func(field string, set bool) {
		if set {
			fields = append(fields, field)
		}
	}

	add("id", selector.ID != "")
	add("name", selector.Name != "")
	add("namespace", selector.Namespace != "")
	add("agent", selector.Agent != "")
	add("types", len(selector.Types) > 0)
	add("statuses", len(selector.Statuses) > 0)
	add("health", selector.Health != "")
	add("tagSelector", selector.TagSelector != "")
	add("labelSelector", selector.LabelSelector != "")
	add("fieldSelector", selector.FieldSelector != "")
	add("scope", selector.Scope != "")
	add("search", selector.Search != "")
	add("cache", selector.Cache != "")
	add("limit", selector.Limit != 0)
	add("includeDeleted", selector.IncludeDeleted)
	return fields
}
