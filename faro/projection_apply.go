package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/google/cel-go/cel"
	"github.com/ohler55/ojg/jp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type ProjectionApplyResult struct {
	Projection string              `json:"projection" yaml:"projection"`
	Target     string              `json:"target" yaml:"target"`
	Sources    int                 `json:"sources" yaml:"sources"`
	Matched    int                 `json:"matched" yaml:"matched"`
	Changed    []string            `json:"changed" yaml:"changed"`
	Created    []string            `json:"created" yaml:"created"`
	Missing    []string            `json:"missing" yaml:"missing"`
	Stale      []string            `json:"stale" yaml:"stale"`
	Warnings   []ProjectionWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	DryRun     bool                `json:"dry_run" yaml:"dry_run"`
}

type compiledProjection struct {
	env      *cel.Env
	filter   cel.Program
	matches  []cel.Program
	mappings map[string]compiledProjectionSet
}

type compiledProjectionSet struct {
	config ProjectionSet
	value  cel.Program
	when   cel.Program
	match  cel.Program
}

func compileProjection(projection Projection) (*compiledProjection, error) {
	env, err := newProjectionEnv()
	if err != nil {
		return nil, err
	}
	compiled := &compiledProjection{env: env, mappings: map[string]compiledProjectionSet{}}
	if projection.Spec.Target != nil && projection.Spec.Target.Filter != "" {
		compiled.filter, err = compileProjectionExpression(env, projection.Spec.Target.Filter)
		if err != nil {
			return nil, fmt.Errorf("spec.target.filter: %w", err)
		}
	}
	for index, expression := range projection.Spec.Match {
		program, err := compileProjectionExpression(env, expression)
		if err != nil {
			return nil, fmt.Errorf("spec.match[%d]: %w", index, err)
		}
		compiled.matches = append(compiled.matches, program)
	}
	for path, mapping := range projection.Spec.Set {
		if _, err := jp.ParseString(path); err != nil {
			return nil, fmt.Errorf("spec.set key %q: %w", path, err)
		}
		value, err := compileProjectionExpression(env, mapping.Value)
		if err != nil {
			return nil, fmt.Errorf("spec.set[%q].value: %w", path, err)
		}
		entry := compiledProjectionSet{config: mapping, value: value}
		if mapping.When != "" {
			entry.when, err = compileProjectionExpression(env, mapping.When)
			if err != nil {
				return nil, fmt.Errorf("spec.set[%q].when: %w", path, err)
			}
		}
		if mapping.Match != "" {
			entry.match, err = compileProjectionExpression(env, mapping.Match)
			if err != nil {
				return nil, fmt.Errorf("spec.set[%q].match: %w", path, err)
			}
		}
		compiled.mappings[path] = entry
	}
	return compiled, nil
}

func applyProjection(projection Projection, source projectionSourceResult, dryRun bool) (ProjectionApplyResult, error) {
	result := ProjectionApplyResult{
		Projection: projection.Metadata.Name,
		Sources:    len(source.Items),
		Changed:    []string{},
		Created:    []string{},
		Missing:    []string{},
		Stale:      []string{},
		Warnings:   source.Warnings,
		DryRun:     dryRun,
	}
	if projection.Spec.Target == nil {
		return result, fmt.Errorf("projection %s has no target", projection.Metadata.Name)
	}
	targetPath := projection.resolvePath(projection.Spec.Target.Path)
	result.Target = targetPath
	body, err := os.ReadFile(targetPath)
	if err != nil {
		return result, err
	}
	if err := validateProjectionTarget(projection, body); err != nil {
		return result, fmt.Errorf("projection %s target before apply: %w", projection.Metadata.Name, err)
	}
	_, file, targets, err := projectionTarget(body, projection.Spec.Target.Select)
	if err != nil {
		return result, err
	}
	compiled, err := compileProjection(projection)
	if err != nil {
		return result, err
	}

	claimed := map[int]string{}
	for _, sourceItem := range source.Items {
		matches, err := matchingProjectionTargets(compiled, sourceItem, targets, source.Context)
		if err != nil {
			return result, err
		}
		identity := projectionItemIdentity(sourceItem)
		if len(matches) == 0 {
			if !projection.Spec.Target.CreatesTargets() {
				result.Missing = append(result.Missing, identity)
				continue
			}
			target, err := createProjectionTarget(compiled, file, projection.Spec.Target.Select, sourceItem, source.Context)
			if err != nil {
				return result, fmt.Errorf("projection %s source %s: %w", projection.Metadata.Name, identity, err)
			}
			targets = append(targets, target)
			claimed[len(targets)-1] = identity
			result.Created = append(result.Created, identity)
			continue
		}
		if len(matches) > 1 {
			return result, fmt.Errorf("projection %s source %s matches target indexes %v", projection.Metadata.Name, identity, matches)
		}
		index := matches[0]
		if previous, ok := claimed[index]; ok {
			return result, fmt.Errorf("projection %s target index %d is matched by both %s and %s", projection.Metadata.Name, index, previous, identity)
		}
		claimed[index] = identity
		result.Matched++
		changes, err := applyProjectionMappings(compiled, file, projection.Spec.Target.Select, index, sourceItem, targets[index], source.Context)
		if err != nil {
			return result, fmt.Errorf("projection %s source %s: %w", projection.Metadata.Name, identity, err)
		}
		for _, path := range changes {
			result.Changed = append(result.Changed, identity+" "+path)
		}
	}
	for index, target := range targets {
		eligible, err := projectionTargetEligible(compiled, target, source.Context)
		if err != nil {
			return result, fmt.Errorf("projection %s target index %d filter: %w", projection.Metadata.Name, index, err)
		}
		if eligible && compiled.filter != nil {
			if _, ok := claimed[index]; ok {
				continue
			}
			result.Stale = append(result.Stale, projectionItemIdentity(target))
		}
	}
	sort.Strings(result.Changed)
	sort.Strings(result.Created)
	sort.Strings(result.Missing)
	sort.Strings(result.Stale)
	rendered := []byte(file.String())
	if _, _, _, err := projectionTarget(rendered, projection.Spec.Target.Select); err != nil {
		return result, fmt.Errorf("projection %s rendered target: %w", projection.Metadata.Name, err)
	}
	if err := validateProjectionTarget(projection, rendered); err != nil {
		return result, fmt.Errorf("projection %s target after apply: %w", projection.Metadata.Name, err)
	}
	if !dryRun && (len(result.Changed) > 0 || len(result.Created) > 0) {
		info, err := os.Stat(targetPath)
		if err != nil {
			return result, err
		}
		if err := os.WriteFile(targetPath, rendered, info.Mode().Perm()); err != nil {
			return result, err
		}
	}
	return result, nil
}

func projectionTarget(body []byte, selector string) (map[string]any, *ast.File, []map[string]any, error) {
	var document map[string]any
	if err := yaml.Unmarshal(body, &document); err != nil {
		return nil, nil, nil, projectionYAMLError(err)
	}
	path, err := jp.ParseString(selector)
	if err != nil {
		return nil, nil, nil, err
	}
	values := path.Get(document)
	targets := make([]map[string]any, 0, len(values))
	for index, value := range values {
		target, ok := value.(map[string]any)
		if !ok {
			return nil, nil, nil, fmt.Errorf("%s result %d is %T, expected mapping", selector, index, value)
		}
		targets = append(targets, target)
	}
	file, err := parser.ParseBytes(body, 0)
	if err != nil {
		return nil, nil, nil, projectionYAMLError(err)
	}
	return document, file, targets, nil
}

func matchingProjectionTargets(compiled *compiledProjection, source map[string]any, targets []map[string]any, projectionContext map[string]any) ([]int, error) {
	var matches []int
	for index, target := range targets {
		eligible, err := projectionTargetEligible(compiled, target, projectionContext)
		if err != nil {
			return nil, fmt.Errorf("filter target index %d: %w", index, err)
		}
		if !eligible {
			continue
		}
		activation := projectionActivation(source, target, projectionContext, nil)
		for _, program := range compiled.matches {
			matched, err := evalProjectionBool(program, activation)
			if err != nil {
				return nil, fmt.Errorf("match target index %d: %w", index, err)
			}
			if matched {
				matches = append(matches, index)
				break
			}
		}
	}
	return matches, nil
}

func projectionTargetEligible(compiled *compiledProjection, target, projectionContext map[string]any) (bool, error) {
	if compiled.filter == nil {
		return true, nil
	}
	return evalProjectionBool(compiled.filter, projectionActivation(map[string]any{}, target, projectionContext, nil))
}

func applyProjectionMappings(compiled *compiledProjection, file *ast.File, selector string, index int, source, target, projectionContext map[string]any) ([]string, error) {
	var changed []string
	evaluationTarget, err := projectionMap(target)
	if err != nil {
		return nil, fmt.Errorf("copy target for expression evaluation: %w", err)
	}
	for _, path := range projectionMappingPaths(compiled) {
		mapping := compiled.mappings[path]
		activation := projectionActivation(source, evaluationTarget, projectionContext, nil)
		if mapping.when != nil {
			apply, err := evalProjectionBool(mapping.when, activation)
			if err != nil {
				return nil, fmt.Errorf("set %s when: %w", path, err)
			}
			if !apply {
				continue
			}
		}
		value, err := evalProjectionValue(mapping.value, activation)
		if err != nil {
			return nil, fmt.Errorf("set %s: %w", path, err)
		}
		jsonPath, _ := jp.ParseString(path)
		current := jsonPath.Get(target)
		if len(current) > 1 {
			return nil, fmt.Errorf("set %s selects %d values; each key must select at most one", path, len(current))
		}
		var currentValue any
		if len(current) == 1 {
			currentValue = current[0]
		}
		value, err = mergeProjectionValue(mapping, currentValue, value, activation)
		if err != nil {
			return nil, fmt.Errorf("set %s: %w", path, err)
		}
		if projectionValuesEqual(currentValue, value) {
			continue
		}
		if err := jsonPath.SetOne(target, value); err != nil {
			return nil, err
		}
		entryPath := strings.TrimSuffix(selector, "[*]") + fmt.Sprintf("[%d]", index)
		flowStyle, err := projectionTargetYAMLFlowStyle(file, entryPath)
		if err != nil {
			return nil, err
		}
		if len(current) == 0 {
			fragment := map[string]any{}
			if err := jsonPath.SetOne(fragment, value); err != nil {
				return nil, err
			}
			if err := mergeProjectionYAML(file, entryPath, fragment, flowStyle); err != nil {
				return nil, err
			}
		} else {
			if err := replaceProjectionYAML(file, entryPath+strings.TrimPrefix(path, "$"), value, flowStyle); err != nil {
				return nil, err
			}
		}
		changed = append(changed, path)
	}
	return changed, nil
}

func projectionMappingPaths(compiled *compiledProjection) []string {
	paths := make([]string, 0, len(compiled.mappings))
	for path := range compiled.mappings {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func projectionTargetYAMLFlowStyle(file *ast.File, path string) (bool, error) {
	yamlPath, err := yaml.PathString(path)
	if err != nil {
		return false, projectionYAMLError(err)
	}
	node, err := yamlPath.FilterFile(file)
	if err != nil {
		return false, projectionYAMLError(err)
	}
	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		return false, fmt.Errorf("%s is %T, expected YAML mapping", path, node)
	}
	return mapping.IsFlowStyle, nil
}

func replaceProjectionYAML(file *ast.File, path string, value any, flowStyle bool) error {
	yamlPath, err := yaml.PathString(path)
	if err != nil {
		return projectionYAMLError(err)
	}
	rendered, err := yaml.MarshalWithOptions(value, yaml.Flow(flowStyle))
	if err != nil {
		return projectionYAMLError(err)
	}
	return projectionYAMLError(yamlPath.ReplaceWithReader(file, bytes.NewReader(rendered)))
}

func mergeProjectionYAML(file *ast.File, path string, value any, flowStyle bool) error {
	yamlPath, err := yaml.PathString(path)
	if err != nil {
		return projectionYAMLError(err)
	}
	rendered, err := yaml.MarshalWithOptions(value, yaml.Flow(flowStyle))
	if err != nil {
		return projectionYAMLError(err)
	}
	return projectionYAMLError(yamlPath.MergeFromReader(file, bytes.NewReader(rendered)))
}

func mergeProjectionValue(mapping compiledProjectionSet, current, incoming any, activation map[string]any) (any, error) {
	switch mapping.config.Strategy {
	case "", "replace":
		return incoming, nil
	case "mergeUnique":
		return mergeUniqueProjectionValues(current, incoming)
	case "replaceMatching":
		return replaceMatchingProjectionValues(mapping.match, current, incoming, activation)
	default:
		return nil, fmt.Errorf("unsupported strategy %q", mapping.config.Strategy)
	}
}

func mergeUniqueProjectionValues(current, incoming any) ([]any, error) {
	existing, ok := current.([]any)
	if current == nil {
		existing, ok = []any{}, true
	}
	if !ok {
		return nil, fmt.Errorf("mergeUnique target is %T, expected list", current)
	}
	added, ok := incoming.([]any)
	if !ok {
		return nil, fmt.Errorf("mergeUnique value is %T, expected list", incoming)
	}
	result := append([]any{}, existing...)
	for _, candidate := range added {
		found := false
		for _, value := range result {
			if projectionValuesEqual(value, candidate) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func replaceMatchingProjectionValues(program cel.Program, current, incoming any, activation map[string]any) ([]any, error) {
	existing, ok := current.([]any)
	if current == nil {
		existing, ok = []any{}, true
	}
	if !ok {
		return nil, fmt.Errorf("replaceMatching target is %T, expected list", current)
	}
	added, ok := incoming.([]any)
	if !ok {
		return nil, fmt.Errorf("replaceMatching value is %T, expected list", incoming)
	}
	result := make([]any, 0, len(existing)+len(added))
	replaced := false
	for _, item := range existing {
		activation["item"] = item
		matched, err := evalProjectionBool(program, activation)
		if err != nil {
			return nil, err
		}
		if matched {
			if !replaced {
				result = append(result, added...)
				replaced = true
			}
			continue
		}
		result = append(result, item)
	}
	if !replaced {
		result = append(result, added...)
	}
	return result, nil
}

func projectionActivation(source, target, projectionContext map[string]any, item any) map[string]any {
	return map[string]any{"source": source, "target": target, "context": projectionContext, "item": item}
}

func projectionValuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func projectionItemIdentity(item map[string]any) string {
	for _, key := range []string{"id", "external_user_id", "name", "repository"} {
		if value := strings.TrimSpace(fmt.Sprint(item[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	body, _ := json.Marshal(item)
	return string(body)
}

func validateProjectionTarget(projection Projection, body []byte) error {
	if projection.Spec.Target == nil || projection.Spec.Target.Schema == "" {
		return nil
	}
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(projection.resolvePath(projection.Spec.Target.Schema))
	if err != nil {
		return err
	}
	var document any
	if err := yaml.Unmarshal(body, &document); err != nil {
		return projectionYAMLError(err)
	}
	return schema.Validate(document)
}
