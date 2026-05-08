package manifestcache

import (
	"github.com/flanksource/clicky/rpc"
	"google.golang.org/protobuf/types/known/structpb"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

// ManifestToService translates a plugin's protobuf manifest into the clicky
// RPCService shape used by the cache. The conversion is lossy by design —
// only the fields needed for `--help` rendering and CLI dispatch survive
// (name, version, description, operations[], scope tag, params_schema as
// a JSON-Schema-shaped struct).
func ManifestToService(m *pluginpb.PluginManifest) rpc.RPCService {
	if m == nil {
		return rpc.RPCService{}
	}
	ops := make([]rpc.RPCOperation, 0, len(m.Operations))
	for _, d := range m.Operations {
		ops = append(ops, operationDefToRPC(d))
	}
	return rpc.RPCService{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Operations:  ops,
	}
}

func operationDefToRPC(d *pluginpb.OperationDef) rpc.RPCOperation {
	if d == nil {
		return rpc.RPCOperation{}
	}
	op := rpc.RPCOperation{
		Name:        d.Name,
		Description: d.Description,
	}
	if d.Scope != "" {
		op.Tags = []string{d.Scope}
	}
	op.Schema, op.Parameters = paramsSchemaToRPC(d.ParamsSchema)
	return op
}

// paramsSchemaToRPC interprets a *structpb.Struct as a JSON-Schema fragment
// (top-level "type", "properties", "required") and returns equivalent clicky
// rpc types. Plugins that don't populate params_schema yield an empty schema
// and zero parameters — the dispatcher still accepts free-form `--param k=v`.
func paramsSchemaToRPC(s *structpb.Struct) (rpc.Schema, []rpc.RPCParameter) {
	schema := rpc.Schema{
		Type:       "object",
		Properties: map[string]rpc.Property{},
		Required:   []string{},
	}
	if s == nil || len(s.Fields) == 0 {
		return schema, nil
	}

	if t := stringField(s, "type"); t != "" {
		schema.Type = t
	}
	for _, name := range stringListField(s, "required") {
		schema.Required = append(schema.Required, name)
	}

	props := structField(s, "properties")
	if props == nil {
		return schema, nil
	}
	requiredSet := map[string]bool{}
	for _, name := range schema.Required {
		requiredSet[name] = true
	}
	params := make([]rpc.RPCParameter, 0, len(props.Fields))
	for fieldName, fieldVal := range props.Fields {
		fs := fieldVal.GetStructValue()
		prop := rpc.Property{Type: "string"}
		if fs != nil {
			if t := stringField(fs, "type"); t != "" {
				prop.Type = t
			}
			if desc := stringField(fs, "description"); desc != "" {
				prop.Description = desc
			}
			if def := fs.Fields["default"]; def != nil {
				prop.Default = def.AsInterface()
			}
			for _, e := range stringListField(fs, "enum") {
				prop.Enum = append(prop.Enum, e)
			}
		}
		schema.Properties[fieldName] = prop
		params = append(params, rpc.RPCParameter{
			Name:        fieldName,
			Type:        prop.Type,
			Description: prop.Description,
			Required:    requiredSet[fieldName],
			Default:     prop.Default,
			In:          "query",
		})
	}
	return schema, params
}

func stringField(s *structpb.Struct, key string) string {
	if s == nil {
		return ""
	}
	v, ok := s.Fields[key]
	if !ok {
		return ""
	}
	return v.GetStringValue()
}

func structField(s *structpb.Struct, key string) *structpb.Struct {
	if s == nil {
		return nil
	}
	v, ok := s.Fields[key]
	if !ok {
		return nil
	}
	return v.GetStructValue()
}

func stringListField(s *structpb.Struct, key string) []string {
	if s == nil {
		return nil
	}
	v, ok := s.Fields[key]
	if !ok {
		return nil
	}
	list := v.GetListValue()
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.Values))
	for _, item := range list.Values {
		if str := item.GetStringValue(); str != "" {
			out = append(out, str)
		}
	}
	return out
}
