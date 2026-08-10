package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/ohler55/ojg/jp"
)

func createProjectionTarget(
	compiled *compiledProjection,
	file *ast.File,
	selector string,
	source map[string]any,
	projectionContext map[string]any,
) (map[string]any, error) {
	target := map[string]any{}
	evaluationTarget := map[string]any{}
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
		value, err = mergeProjectionValue(mapping, nil, value, activation)
		if err != nil {
			return nil, fmt.Errorf("set %s: %w", path, err)
		}
		jsonPath, _ := jp.ParseString(path)
		if err := jsonPath.SetOne(target, value); err != nil {
			return nil, fmt.Errorf("set %s: %w", path, err)
		}
	}
	if len(target) == 0 {
		return nil, fmt.Errorf("spec.set produced an empty target")
	}

	sequenceSelector := strings.TrimSuffix(selector, "[*]")
	sequencePath, err := yaml.PathString(sequenceSelector)
	if err != nil {
		return nil, projectionYAMLError(err)
	}
	sequenceNode, err := sequencePath.FilterFile(file)
	if err != nil {
		return nil, projectionYAMLError(err)
	}
	sequence, ok := sequenceNode.(*ast.SequenceNode)
	if !ok {
		return nil, fmt.Errorf("%s is %T, expected YAML sequence", sequenceSelector, sequenceNode)
	}
	rendered, err := yaml.MarshalWithOptions([]map[string]any{target}, yaml.Flow(sequence.IsFlowStyle))
	if err != nil {
		return nil, projectionYAMLError(err)
	}
	if err := sequencePath.MergeFromReader(file, bytes.NewReader(rendered)); err != nil {
		return nil, projectionYAMLError(err)
	}
	return target, nil
}
