package v1

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

func TestCRDNoUnexportedFields(t *testing.T) {
	crds := []struct {
		name string
		obj  any
	}{
		{"Application", Application{}},
		{"Connection", Connection{}},
		{"IncidentRule", IncidentRule{}},
		{"Notification", Notification{}},
		{"NotificationSilence", NotificationSilence{}},
		{"Permission", Permission{}},
		{"PermissionGroup", PermissionGroup{}},
		{"Playbook", Playbook{}},
		{"Scope", Scope{}},
		{"Team", Team{}},
		{"View", View{}},
	}

	for _, tc := range crds {
		t.Run(tc.name, func(t *testing.T) {
			unexported := findUnexportedFields(reflect.TypeOf(tc.obj), "", nil)
			if len(unexported) > 0 {
				t.Errorf("has unexported fields:\n  %s", strings.Join(unexported, "\n  "))
			}
		})
	}
}

func findUnexportedFields(t reflect.Type, prefix string, visited map[reflect.Type]bool) []string {
	if visited == nil {
		visited = make(map[reflect.Type]bool)
	}

	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil
	}

	if visited[t] {
		return nil
	}
	visited[t] = true

	// Skip types that handle their own serialization
	if t.Implements(jsonMarshalerType) || reflect.PointerTo(t).Implements(jsonMarshalerType) {
		return nil
	}


	var result []string
	for i := range t.NumField() {
		field := t.Field(i)

		if field.Anonymous {
			result = append(result, findUnexportedFields(field.Type, prefix, visited)...)
			continue
		}

		fieldPath := prefix + field.Name
		if !field.IsExported() {
			result = append(result, fieldPath)
			continue
		}

		result = append(result, findUnexportedFields(field.Type, fieldPath+".", visited)...)
	}

	return result
}
