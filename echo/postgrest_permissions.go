package echo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

// ResourceType represents the type of resource for permission filtering
type ResourceType int

const (
	ResourceTypePlaybooks ResourceType = iota
	ResourceTypeConnections
	ResourceTypeConfigs
	ResourceTypeComponents
)

// String returns the string representation of the ResourceType
func (r ResourceType) String() string {
	switch r {
	case ResourceTypePlaybooks:
		return "playbooks"
	case ResourceTypeConnections:
		return "connections"
	case ResourceTypeConfigs:
		return "configs"
	case ResourceTypeComponents:
		return "components"
	default:
		return "unknown"
	}
}

// ResourceSelectorField represents which fields from ResourceSelector should be extracted
type ResourceSelectorField string

const (
	ResourceSelectorID        ResourceSelectorField = "ID"
	ResourceSelectorName      ResourceSelectorField = "Name"
	ResourceSelectorNamespace ResourceSelectorField = "Namespace"
)

// PermissionFilterConfig configures how to extract and apply permission filters
type PermissionFilterConfig struct {
	// ResourceType determines which selector field to use from dutyRBAC.Selectors
	ResourceType ResourceType
	// Fields defines which ResourceSelector fields to extract and their priority order
	Fields []ResourceSelectorField
}

// permissionsToPostgRESTParams converts permissions to PostgREST query parameters generically
func permissionsToPostgRESTParams(ctx context.Context, q url.Values, config PermissionFilterConfig) error {
	roles, err := dutyRBAC.RolesForUser(ctx.User().ID.String())
	if err != nil {
		return fmt.Errorf("failed to get roles for user: %w", err)
	}

	// Only apply filters for guest users
	if !lo.Contains(roles, policy.RoleGuest) {
		return nil
	}

	// Get permissions for the user
	permissions, err := dutyRBAC.PermsForUser(ctx.User().ID.String())
	if err != nil {
		return fmt.Errorf("failed to get permissions for user: %w", err)
	}

	var permissionIDs []string
	for _, p := range permissions {
		if p.Action != policy.ActionRead && p.Action != "*" {
			continue
		}

		if p.Deny {
			continue
		}

		if uuid.Validate(p.ID) == nil {
			permissionIDs = append(permissionIDs, p.ID)
		}
	}

	if len(permissionIDs) == 0 {
		return dutyApi.Errorf(dutyApi.EFORBIDDEN, "guest user %s has no permissions to view %s", ctx.User().ID.String(), config.ResourceType)
	}

	var permModels []models.Permission
	if err := ctx.DB().Where("id IN ?", permissionIDs).Find(&permModels).Error; err != nil {
		return fmt.Errorf("failed to query permissions: %w", err)
	}

	// Map to store filters by field type
	filtersByField := make(map[ResourceSelectorField][]string)

	for _, perm := range permModels {
		if len(perm.ObjectSelector) == 0 {
			continue
		}

		var selectors dutyRBAC.Selectors
		if err := json.Unmarshal(perm.ObjectSelector, &selectors); err != nil {
			return fmt.Errorf("failed to parse object selector: %w", err)
		}

		// Extract the appropriate selector based on resource type
		var resourceSelectors []types.ResourceSelector
		switch config.ResourceType {
		case ResourceTypePlaybooks:
			resourceSelectors = selectors.Playbooks
		case ResourceTypeConnections:
			resourceSelectors = selectors.Connections
		case ResourceTypeConfigs:
			resourceSelectors = selectors.Configs
		case ResourceTypeComponents:
			resourceSelectors = selectors.Components
		default:
			return fmt.Errorf("unsupported resource type: %s", config.ResourceType)
		}

		// Extract field values from each selector
		for _, selector := range resourceSelectors {
			for _, field := range config.Fields {
				value := extractSelectorField(selector, field)
				if value != "" {
					filtersByField[field] = append(filtersByField[field], value)
				}
			}
		}
	}

	// Apply filters in priority order (first non-empty field wins)
	for _, field := range config.Fields {
		if filters := filtersByField[field]; len(filters) > 0 {
			fieldName := strings.ToLower(string(field))
			q.Set(fieldName, fmt.Sprintf("in.(%s)", strings.Join(lo.Uniq(filters), ",")))
			break // Only apply the first non-empty filter set
		}
	}

	return nil
}

// extractSelectorField extracts the specified field value from a ResourceSelector
func extractSelectorField(selector types.ResourceSelector, field ResourceSelectorField) string {
	switch field {
	case ResourceSelectorID:
		return selector.ID
	case ResourceSelectorName:
		return selector.Name
	case ResourceSelectorNamespace:
		return selector.Namespace
	default:
		return ""
	}
}

// applyPlaybookPermissionFilters applies permission-based filters for playbooks
func applyPlaybookPermissionFilters(ctx context.Context, q url.Values) error {
	return permissionsToPostgRESTParams(ctx, q, PermissionFilterConfig{
		ResourceType: ResourceTypePlaybooks,
		Fields: []ResourceSelectorField{
			ResourceSelectorID,
			ResourceSelectorNamespace,
			ResourceSelectorName,
		},
	})
}
