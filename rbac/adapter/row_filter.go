package adapter

import (
	"fmt"
	"maps"
	"slices"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/types"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// rowFilterFields are the selector fields row filters can match on each table.
// A Config's namespace is its namespace tag, so it's matched as a tag.
var rowFilterFields = map[string][]string{
	policy.ResourceConfig:    {"id", "name", "namespace", "agent", "tagSelector"},
	policy.ResourceComponent: {"id", "name", "agent"},
	policy.ResourceCheck:     {"id", "name", "agent"},
	policy.ResourceCanary:    {"id", "name", "agent"},
	policy.ResourcePlaybook:  {"id", "name"},
}

// rowFilterKinds are the resource types whose tables are filtered by row.
var rowFilterKinds = []string{policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck, policy.ResourceCanary, policy.ResourcePlaybook}

// RowFilter converts a selector to a row filter (rls.Scope) of its resource type's table.
//
// It returns an error when row filters can't match the selector exactly, and a nil filter when the
// selector can't match any row. Connections have no row filters: only name: "*" is accepted, which
// grants every connection.
func RowFilter(kind string, selector types.ResourceSelector) (*rls.Scope, error) {
	if kind == policy.ResourceConnection {
		if !selector.Wildcard() {
			return nil, fmt.Errorf(`connections can't be filtered by row, so only name: "*" is supported`)
		}
		return &rls.Scope{ID: "*"}, nil
	}

	supported, ok := rowFilterFields[kind]
	if !ok {
		return nil, fmt.Errorf("%s can't be filtered by row", kind)
	}

	for _, field := range []struct {
		name string
		set  bool
	}{
		{"id", selector.ID != ""},
		{"name", selector.Name != ""},
		{"namespace", selector.Namespace != ""},
		{"agent", selector.Agent != ""},
		{"types", len(selector.Types) > 0},
		{"tagSelector", selector.TagSelector != ""},
		{"labelSelector", selector.LabelSelector != ""},
	} {
		if field.set && !slices.Contains(supported, field.name) {
			return nil, fmt.Errorf("row filters can't match a %s's %s", kind, field.name)
		}
	}

	var filter rls.Scope
	if selector.Name != "" && selector.Name != "*" {
		if types.IsMatchItem(selector.Name) {
			return nil, fmt.Errorf("name %q must be exact or *", selector.Name)
		}
		filter.Names = []string{selector.Name}
	}

	if selector.ID != "" {
		filter.ID = selector.ID
	}

	if selector.Agent != "" {
		filter.Agents = []string{selector.Agent}
	}

	tags := map[string]string{}
	if selector.TagSelector != "" {
		var err error
		if tags, err = equalityTags(selector.TagSelector); err != nil {
			return nil, err
		}
	}

	if selector.Namespace != "" {
		if types.IsMatchItem(selector.Namespace) {
			return nil, fmt.Errorf("namespace %q must be exact", selector.Namespace)
		}
		if existing, ok := tags["namespace"]; ok && existing != selector.Namespace {
			return nil, nil
		}
		tags["namespace"] = selector.Namespace
	}

	if len(tags) > 0 {
		filter.Tags = tags
	}

	// "*" matches every value, including empty ones, so it adds no condition.
	// Row filters need at least one condition to match anything.
	if filter.IsEmpty() {
		filter.ID = "*"
	}

	return &filter, nil
}

// equalityTags returns the tags a tagSelector requires. Only key=value requirements are supported.
func equalityTags(tagSelector string) (map[string]string, error) {
	parsed, err := labels.Parse(tagSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid tagSelector %q: %w", tagSelector, err)
	}

	requirements, _ := parsed.Requirements()
	tags := map[string]string{}
	for _, r := range requirements {
		if (r.Operator() != selection.Equals && r.Operator() != selection.DoubleEquals) || r.Values().Len() != 1 {
			return nil, fmt.Errorf("tagSelector %q must only use key=value", tagSelector)
		}

		if existing, ok := tags[r.Key()]; ok && existing != r.Values().List()[0] {
			return nil, fmt.Errorf("tagSelector %q can't match any resource", tagSelector)
		}
		tags[r.Key()] = r.Values().List()[0]
	}

	return tags, nil
}

// IntersectRowFilters returns a filter that matches the rows matched by both filters.
// ok is false when no row can match both.
func IntersectRowFilters(a, b rls.Scope) (rls.Scope, bool) {
	var result rls.Scope
	var ok bool

	if result.Names, ok = intersectValues(a.Names, b.Names); !ok {
		return result, false
	}

	if result.Agents, ok = intersectValues(a.Agents, b.Agents); !ok {
		return result, false
	}

	ids, ok := intersectValues(nonEmpty(a.ID), nonEmpty(b.ID))
	if !ok {
		return result, false
	} else if len(ids) > 0 {
		result.ID = ids[0]
	}

	if len(a.Tags) > 0 || len(b.Tags) > 0 {
		result.Tags = maps.Clone(a.Tags)
		if result.Tags == nil {
			result.Tags = map[string]string{}
		}
		for k, v := range b.Tags {
			if existing, found := result.Tags[k]; found && existing != v {
				return result, false
			}
			result.Tags[k] = v
		}
	}

	return result, true
}

// intersectValues intersects single-valued (or empty, meaning any) constraints, where * matches any value.
func intersectValues(a, b []string) ([]string, bool) {
	switch {
	case len(a) == 0:
		return b, true
	case len(b) == 0:
		return a, true
	case a[0] == "*":
		return b, true
	case b[0] == "*":
		return a, true
	case a[0] == b[0]:
		return a, true
	default:
		return nil, false
	}
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
