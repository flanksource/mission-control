package catalog

import (
	"fmt"
	"reflect"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	reportAPI "github.com/flanksource/incident-commander/api"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

type changeMapper struct {
	mappings []compiledCategoryMapping
}

type compiledCategoryMapping struct {
	mapping   reportAPI.CatalogReportCategoryMapping
	filter    cel.Program
	transform cel.Program
}

func newChangeMapper(ctx dutyContext.Context, mappings []reportAPI.CatalogReportCategoryMapping) (*changeMapper, error) {
	if len(mappings) == 0 {
		return nil, nil
	}

	envOptions := []cel.EnvOption{
		cel.Variable("change", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("details", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("typedChange", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("artifacts", cel.ListType(cel.DynType)),
		cel.Variable("id", cel.StringType),
		cel.Variable("configID", cel.StringType),
		cel.Variable("configName", cel.StringType),
		cel.Variable("configType", cel.StringType),
		cel.Variable("permalink", cel.StringType),
		cel.Variable("changeType", cel.StringType),
		cel.Variable("category", cel.StringType),
		cel.Variable("severity", cel.StringType),
		cel.Variable("source", cel.StringType),
		cel.Variable("summary", cel.StringType),
		cel.Variable("createdBy", cel.StringType),
		cel.Variable("externalCreatedBy", cel.StringType),
		cel.Variable("createdAt", cel.StringType),
		cel.Variable("count", cel.IntType),
	}

	for _, fn := range dutyContext.CelEnvFuncs {
		envOptions = append(envOptions, fn(ctx))
	}

	env, err := cel.NewEnv(envOptions...)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	compiled := make([]compiledCategoryMapping, 0, len(mappings))
	for i, mapping := range mappings {
		if mapping.Filter == "" {
			return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "categoryMappings[%d] filter is required", i)
		}
		if mapping.Category == "" && mapping.Transform == "" {
			return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "categoryMappings[%d] must define category or transform", i)
		}

		filter, err := compileChangeMappingProgram(env, mapping.Filter)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to compile categoryMappings[%d] filter", i)
		}

		var transform cel.Program
		if mapping.Transform != "" {
			transform, err = compileChangeMappingProgram(env, mapping.Transform)
			if err != nil {
				return nil, ctx.Oops().Wrapf(err, "failed to compile categoryMappings[%d] transform", i)
			}
		}

		compiled = append(compiled, compiledCategoryMapping{
			mapping:   mapping,
			filter:    filter,
			transform: transform,
		})
	}

	return &changeMapper{mappings: compiled}, nil
}

func compileChangeMappingProgram(env *cel.Env, expression string) (cel.Program, error) {
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}

	return env.Program(ast)
}

func (m *changeMapper) Apply(change *reportAPI.CatalogReportChange) error {
	if change == nil {
		return nil
	}

	env := changeMappingEnv(change)
	category := change.Category
	typedChange := change.TypedChange

	if m != nil {
		for _, mapping := range m.mappings {
			matched, err := evalMappingFilter(mapping.filter, env)
			if err != nil {
				return fmt.Errorf("failed to evaluate filter %q: %w", mapping.mapping.Filter, err)
			}
			if !matched {
				continue
			}

			if category == "" && mapping.mapping.Category != "" {
				category = mapping.mapping.Category
				env["category"] = category
				changeEnv := env["change"].(map[string]any)
				changeEnv["category"] = category
			}

			if typedChange == nil && mapping.transform != nil {
				typedChange, err = evalMappingTransform(mapping.transform, env)
				if err != nil {
					return fmt.Errorf("failed to evaluate transform %q: %w", mapping.mapping.Transform, err)
				}
				if typedChange != nil {
					env["typedChange"] = typedChange
					changeEnv := env["change"].(map[string]any)
					changeEnv["typedChange"] = typedChange
				}
			}

			if category != "" && typedChange != nil {
				break
			}
		}
	}

	if change.Category == "" {
		change.Category = category
	}
	if change.TypedChange == nil {
		if typedChange != nil {
			change.TypedChange = typedChange
		} else {
			change.TypedChange = typedChangeFromDetails(change.Details)
		}
	}

	return nil
}

func evalMappingFilter(program cel.Program, env map[string]any) (bool, error) {
	out, _, err := program.Eval(env)
	if err != nil {
		return false, err
	}

	value, ok := celValueToNative(out).(bool)
	if !ok {
		return false, fmt.Errorf("filter returned %T, expected bool", celValueToNative(out))
	}

	return value, nil
}

func evalMappingTransform(program cel.Program, env map[string]any) (map[string]any, error) {
	out, _, err := program.Eval(env)
	if err != nil {
		return nil, err
	}

	value, ok := celValueToNative(out).(map[string]any)
	if !ok {
		return nil, nil
	}

	kind, _ := value["kind"].(string)
	if kind == "" {
		return nil, nil
	}

	return value, nil
}

func typedChangeFromDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}

	kind, _ := details["kind"].(string)
	if kind == "" {
		return nil
	}

	return celValueToNative(details).(map[string]any)
}

func changeMappingEnv(change *reportAPI.CatalogReportChange) map[string]any {
	details := map[string]any{}
	if change.Details != nil {
		details = celValueToNative(change.Details).(map[string]any)
	}

	typedChange := map[string]any{}
	if change.TypedChange != nil {
		typedChange = celValueToNative(change.TypedChange).(map[string]any)
	}

	artifacts := make([]any, 0, len(change.Artifacts))
	for _, artifact := range change.Artifacts {
		artifacts = append(artifacts, map[string]any{
			"id":          artifact.ID,
			"filename":    artifact.Filename,
			"contentType": artifact.ContentType,
			"size":        artifact.Size,
			"dataUri":     artifact.DataURI,
		})
	}

	changeEnv := map[string]any{
		"id":                change.ID,
		"configID":          change.ConfigID,
		"configName":        change.ConfigName,
		"configType":        change.ConfigType,
		"permalink":         change.Permalink,
		"changeType":        change.ChangeType,
		"category":          change.Category,
		"severity":          change.Severity,
		"source":            change.Source,
		"summary":           change.Summary,
		"details":           details,
		"typedChange":       typedChange,
		"createdBy":         change.CreatedBy,
		"externalCreatedBy": change.ExternalCreatedBy,
		"createdAt":         change.CreatedAt,
		"count":             int64(change.Count),
		"artifacts":         artifacts,
	}

	return map[string]any{
		"change":            changeEnv,
		"details":           details,
		"typedChange":       typedChange,
		"artifacts":         artifacts,
		"id":                change.ID,
		"configID":          change.ConfigID,
		"configName":        change.ConfigName,
		"configType":        change.ConfigType,
		"permalink":         change.Permalink,
		"changeType":        change.ChangeType,
		"category":          change.Category,
		"severity":          change.Severity,
		"source":            change.Source,
		"summary":           change.Summary,
		"createdBy":         change.CreatedBy,
		"externalCreatedBy": change.ExternalCreatedBy,
		"createdAt":         change.CreatedAt,
		"count":             int64(change.Count),
	}
}

func celValueToNative(value any) any {
	switch v := value.(type) {
	case nil, bool, string, int, int32, int64, uint, uint32, uint64, float32, float64:
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = celValueToNative(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, celValueToNative(item))
		}
		return out
	case ref.Val:
		return celValueToNative(v.Value())
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil
	}

	switch rv.Kind() {
	case reflect.Map:
		out := map[string]any{}
		for _, key := range rv.MapKeys() {
			out[fmt.Sprint(celValueToNative(key.Interface()))] = celValueToNative(rv.MapIndex(key).Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, celValueToNative(rv.Index(i).Interface()))
		}
		return out
	}

	return value
}
