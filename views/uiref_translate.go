package views

import (
	"fmt"
	"strings"

	"github.com/flanksource/incident-commander/api"
)

// translateViewSections translates UIRef filter formats from CRD (user-friendly)
// to UI internal format.
func translateViewSections(sections []api.ViewSection) []api.ViewSection {
	result := make([]api.ViewSection, len(sections))
	for i, section := range sections {
		result[i] = translateViewSection(section)
	}
	return result
}

// translateViewSection translates a single section's UIRef filters.
func translateViewSection(section api.ViewSection) api.ViewSection {
	if section.UIRef == nil {
		return section
	}

	return api.ViewSection{
		Title:   section.Title,
		Icon:    section.Icon,
		ViewRef: section.ViewRef,
		UIRef:   translateUIRef(section.UIRef),
	}
}

// translateUIRef translates filter formats from CRD to UI internal format.
func translateUIRef(uiRef *api.UIRef) *api.UIRef {
	if uiRef == nil {
		return nil
	}

	result := &api.UIRef{}

	if uiRef.Changes != nil {
		result.Changes = translateChangesFilters(uiRef.Changes)
	}

	if uiRef.Configs != nil {
		result.Configs = translateConfigsFilters(uiRef.Configs)
	}

	return result
}

// translateChangesFilters converts user-friendly CRD format to UI internal format.
//
// Tags: "env=prod,!env=staging" → "env____prod:1,env____staging:-1"
// Tristate: "diff,-BackOff" → "diff:1,BackOff:-1"
func translateChangesFilters(filters *api.ChangesUIFilters) *api.ChangesUIFilters {
	if filters == nil {
		return nil
	}

	return &api.ChangesUIFilters{
		// ConfigTypes: replace "::" with "__" as the UI uses "__" for type separators
		ConfigTypes: translateTristate(filters.ConfigTypes, "::", "__"),
		ChangeType:  translateTristate(filters.ChangeType, "", ":"),
		Severity:    filters.Severity,
		From:        filters.From,
		To:          filters.To,
		Tags:        translateTags(filters.Tags),
		Source:      translateTristate(filters.Source, "", ":"),
		Summary:     translateTristate(filters.Summary, "", ":"),
		CreatedBy:   translateTristate(filters.CreatedBy, "", ":"),
	}
}

// translateConfigsFilters converts user-friendly CRD format to UI internal format.
func translateConfigsFilters(filters *api.ConfigsUIFilters) *api.ConfigsUIFilters {
	if filters == nil {
		return nil
	}

	return &api.ConfigsUIFilters{
		Search:     filters.Search,
		ConfigType: filters.ConfigType,
		Labels:     translateTags(filters.Labels),
		Status:     translateTristate(filters.Status, "", ":"),
		Health:     translateTristate(filters.Health, "", ":"),
	}
}

// translateTristate converts user-friendly tristate format to UI internal format.
//
// User format: "value1,-value2,value3"
// UI format:   "value1:1,value2:-1,value3:1"
//
// The separator parameter specifies what to replace "::" with (for config types).
func translateTristate(input, oldSep, newSep string) string {
	if input == "" {
		return ""
	}

	values := strings.Split(input, ",")
	var result []string

	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}

		// Check if it's an exclusion (starts with -)
		if strings.HasPrefix(v, "-") {
			value := v[1:] // Remove the leading -
			if oldSep != "" && newSep != "" {
				value = strings.ReplaceAll(value, oldSep, newSep)
			}
			result = append(result, fmt.Sprintf("%s:-1", value))
		} else {
			value := v
			if oldSep != "" && newSep != "" {
				value = strings.ReplaceAll(value, oldSep, newSep)
			}
			result = append(result, fmt.Sprintf("%s:1", value))
		}
	}

	return strings.Join(result, ",")
}

// translateTags converts Kubernetes-style label selector to UI internal format.
//
// User format: "key=value,!key=value2" (Kubernetes label selector)
// UI format:   "key____value:1,key____value2:-1"
func translateTags(input string) string {
	if input == "" {
		return ""
	}

	// Split by comma, but handle the case where values might contain commas
	values := strings.Split(input, ",")
	var result []string

	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}

		// Check if it's an exclusion (starts with !)
		exclude := false
		if strings.HasPrefix(v, "!") {
			exclude = true
			v = v[1:] // Remove the leading !
		}

		// Split by = to get key and value
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			// Invalid format, skip
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// UI internal format uses ____ as separator
		tagKey := fmt.Sprintf("%s____%s", key, value)
		if exclude {
			result = append(result, fmt.Sprintf("%s:-1", tagKey))
		} else {
			result = append(result, fmt.Sprintf("%s:1", tagKey))
		}
	}

	return strings.Join(result, ",")
}
