// Package manifestadapter converts internal plugin manifests into client API data.
package manifestadapter

import (
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/plugin/api"
)

// ManifestToService projects a plugin manifest onto its client-visible fields.
func ManifestToService(manifest *api.PluginManifest) clientapi.PluginService {
	if manifest == nil {
		return clientapi.PluginService{}
	}
	operations := make([]clientapi.PluginOperation, 0, len(manifest.Operations))
	for _, definition := range manifest.Operations {
		operations = append(operations, operationDefToPlugin(definition))
	}
	return clientapi.PluginService{
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Operations:  operations,
	}
}

func operationDefToPlugin(definition *api.OperationDef) clientapi.PluginOperation {
	if definition == nil {
		return clientapi.PluginOperation{}
	}
	operation := clientapi.PluginOperation{
		Name:        definition.Name,
		Description: definition.Description,
	}
	if definition.Scope != "" {
		operation.Tags = []string{definition.Scope}
	}
	operation.Schema, operation.Parameters = paramsSchemaToPlugin(definition.ParamsSchema)
	return operation
}

func paramsSchemaToPlugin(value *structpb.Struct) (clientapi.Schema, []clientapi.PluginParameter) {
	schema := clientapi.Schema{
		Type:       "object",
		Properties: map[string]clientapi.Property{},
		Required:   []string{},
	}
	if value == nil || len(value.Fields) == 0 {
		return schema, nil
	}

	if schemaType := stringField(value, "type"); schemaType != "" {
		schema.Type = schemaType
	}
	schema.Required = append(schema.Required, stringListField(value, "required")...)

	properties := structField(value, "properties")
	if properties == nil {
		return schema, nil
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	parameters := make([]clientapi.PluginParameter, 0, len(properties.Fields))
	for fieldName, fieldValue := range properties.Fields {
		fieldSchema := fieldValue.GetStructValue()
		property := clientapi.Property{Type: "string"}
		if fieldSchema != nil {
			if propertyType := stringField(fieldSchema, "type"); propertyType != "" {
				property.Type = propertyType
			}
			property.Description = stringField(fieldSchema, "description")
			if defaultValue := fieldSchema.Fields["default"]; defaultValue != nil {
				property.Default = defaultValue.AsInterface()
			}
			property.Enum = append(property.Enum, stringListField(fieldSchema, "enum")...)
		}
		schema.Properties[fieldName] = property
		parameters = append(parameters, clientapi.PluginParameter{
			Name:        fieldName,
			Type:        property.Type,
			Description: property.Description,
			Required:    required[fieldName],
			Default:     property.Default,
			In:          "query",
		})
	}
	return schema, parameters
}

func stringField(value *structpb.Struct, key string) string {
	if value == nil {
		return ""
	}
	field, ok := value.Fields[key]
	if !ok {
		return ""
	}
	return field.GetStringValue()
}

func structField(value *structpb.Struct, key string) *structpb.Struct {
	if value == nil {
		return nil
	}
	field, ok := value.Fields[key]
	if !ok {
		return nil
	}
	return field.GetStructValue()
}

func stringListField(value *structpb.Struct, key string) []string {
	if value == nil {
		return nil
	}
	field, ok := value.Fields[key]
	if !ok || field.GetListValue() == nil {
		return nil
	}
	result := make([]string, 0, len(field.GetListValue().Values))
	for _, item := range field.GetListValue().Values {
		if text := item.GetStringValue(); text != "" {
			result = append(result, text)
		}
	}
	return result
}
