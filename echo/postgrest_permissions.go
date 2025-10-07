package echo

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
