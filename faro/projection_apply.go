package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/gomplate/v3"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/ohler55/ojg/jp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// projectionStatus is how one projection ended. It is a domain vocabulary rather than
// task.Status because a projection with no target is skipped, not cancelled, and that
// distinction is the difference between "nothing to do" and "something went wrong".
type projectionStatus string

const (
	projectionApplied projectionStatus = "applied"
	projectionWarned  projectionStatus = "warned"
	projectionSkipped projectionStatus = "skipped"
	projectionFailed  projectionStatus = "failed"
)

func (s projectionStatus) Pretty() api.Text {
	switch s {
	case projectionWarned:
		return api.Text{}.Add(icons.Warning).Space().Append(string(s), "text-yellow-500")
	case projectionSkipped:
		return api.Text{}.Add(icons.Circle).Space().Append(string(s), "text-gray-500")
	case projectionFailed:
		return api.Text{}.Add(icons.Error).Space().Append(string(s), "text-red-500")
	default:
		return api.Text{}.Add(icons.Success).Space().Append(string(s), "text-green-500")
	}
}

type ProjectionApplyResult struct {
	Projection string           `json:"projection" yaml:"projection"`
	Status     projectionStatus `json:"status" yaml:"status"`
	// Error is the reason a failed projection stopped. It is carried on the result
	// rather than returned so one projection's failure cannot hide the others.
	Error   string `json:"error,omitempty" yaml:"error,omitempty"`
	Target  string `json:"target" yaml:"target"`
	Sources int    `json:"sources" yaml:"sources"`
	// Filtered counts sources dropped by spec.source.where, so a predicate that
	// silently matches nothing is visible rather than looking like clean output.
	Filtered int `json:"filtered" yaml:"filtered"`
	Matched  int `json:"matched" yaml:"matched"`
	// Aggregated counts source applications beyond the first for each target,
	// keeping the summary honest about fan-in under spec.target.aggregate.
	Aggregated int                 `json:"aggregated" yaml:"aggregated"`
	Changed    []string            `json:"changed" yaml:"changed"`
	Created    []string            `json:"created" yaml:"created"`
	Missing    []string            `json:"missing" yaml:"missing"`
	Stale      []string            `json:"stale" yaml:"stale"`
	Warnings   []ProjectionWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	DryRun     bool                `json:"dry_run" yaml:"dry_run"`
}

// The table carries counts; the identities behind them live in RowDetail. Printing
// every changed identity as a cell wrapped hundreds of UUIDs across the summary and
// buried the one column that mattered.
func (r ProjectionApplyResult) Columns() []api.ColumnDef {
	columns := []api.ColumnDef{
		{Name: "projection", Label: "Projection", Style: "font-bold"},
		{Name: "status", Label: "Status"},
		{Name: "target", Label: "Target", Style: "text-gray-500"},
		{Name: "sources", Label: "Sources", Type: "int"},
		{Name: "filtered", Label: "Filtered", Type: "int"},
		{Name: "matched", Label: "Matched", Type: "int"},
		{Name: "created", Label: "Created", Type: "int"},
		{Name: "changed", Label: "Changed", Type: "int"},
		{Name: "stale", Label: "Stale", Type: "int"},
		{Name: "missing", Label: "Missing", Type: "int"},
		{Name: "warnings", Label: "Warnings", Type: "int"},
	}
	if projectionTableCarriesError() {
		columns = append(columns, api.ColumnDef{Name: "error", Label: "Error"})
	}
	return columns
}

// projectionTableCarriesError reports the formats where the table is the only place a
// failure can say why. Pretty output states the reason twice already — in the report
// above the table and in RowDetail — and a jsonschema error in a cell collapses every
// other column to a few characters, which is why the column is absent there. JSON and
// YAML serialise the error field directly and never reach Columns at all; CSV, markdown
// and HTML have neither route, so without this they render a bare `failed`.
func projectionTableCarriesError() bool {
	return !rendersPretty(clicky.Flags.FormatOptions)
}

func (r ProjectionApplyResult) Row() map[string]any {
	row := map[string]any{
		"projection": r.Projection,
		"status":     r.Status.Pretty(),
		"target":     targetName(r.Target),
		"sources":    r.Sources,
		"filtered":   r.Filtered,
		"matched":    r.Matched,
		"created":    len(r.Created),
		"changed":    len(r.Changed),
		"stale":      len(r.Stale),
		"missing":    len(r.Missing),
		"warnings":   len(r.Warnings),
	}
	if projectionTableCarriesError() {
		row["error"] = projectionErrorSummary(r.Error)
	}
	return row
}

// projectionErrorSummary flattens a failure onto one line, because a markdown or HTML
// table cell cannot hold the newlines a jsonschema violation arrives with. The unflattened
// text stays on the result's Error field, which JSON and YAML carry verbatim.
func projectionErrorSummary(message string) string {
	return strings.Join(strings.Fields(message), " ")
}

// projectionFailureReport lists why each failed projection failed. The reasons do not
// belong in the summary table: a jsonschema violation runs to dozens of lines, and one
// of those in a cell collapses every other column to a few characters. Returns nil when
// nothing failed.
func projectionFailureReport(results []ProjectionApplyResult) api.Textable {
	report := api.Text{}
	for _, result := range results {
		if result.Status != projectionFailed {
			continue
		}
		report = report.Add(icons.Error).Space().
			Append(result.Projection, "text-red-500 font-bold").NewLine().
			Append(indentProjectionError(result.Error), "text-gray-500").NewLine()
	}
	if len(report.Children) == 0 {
		return nil
	}
	return api.Text{}.Append("Failures", "font-bold").NewLine().Add(report)
}

// projectionErrorLines caps how much of one failure is echoed to the terminal. A
// jsonschema violation reports every offending entry, which for a whole register runs to
// hundreds of lines and buries the summary. The full text stays on the result, so
// --json keeps it.
const projectionErrorLines = 5

func indentProjectionError(message string) string {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	truncated := 0
	if len(lines) > projectionErrorLines {
		truncated = len(lines) - projectionErrorLines
		lines = lines[:projectionErrorLines]
	}
	for index, line := range lines {
		lines[index] = "  " + line
	}
	if truncated > 0 {
		lines = append(lines, fmt.Sprintf("  … %d more lines, see --json", truncated))
	}
	return strings.Join(lines, "\n")
}

// A skipped projection, or one that failed before resolving its target, has no target
// to name — filepath.Base would render that as ".".
func targetName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func (r ProjectionApplyResult) RowDetail() api.Textable {
	detail := api.Text{}
	if r.Error != "" {
		detail = detail.Append(r.Error, "text-red-500").NewLine()
	}
	for _, warning := range r.Warnings {
		detail = detail.Add(icons.Warning).Space().
			Append(warning.Source, "text-yellow-500").Space().Append(warning.Message, "text-gray-500").NewLine()
	}
	for _, section := range []struct {
		label      string
		style      string
		identities []string
	}{
		{"created", "text-green-500", r.Created},
		{"changed", "text-blue-500", r.Changed},
		{"stale", "text-yellow-500", r.Stale},
		{"missing", "text-red-500", r.Missing},
	} {
		if len(section.identities) == 0 {
			continue
		}
		detail = detail.Append(section.label, section.style).NewLine()
		for _, identity := range section.identities {
			detail = detail.Append("  "+identity, "text-gray-500").NewLine()
		}
	}
	if len(detail.Children) == 0 {
		return nil
	}
	return detail
}

type compiledProjection struct {
	where    *gomplate.Template
	filter   *gomplate.Template
	matches  []*gomplate.Template
	mappings map[string]compiledProjectionSet
}

type compiledProjectionSet struct {
	config ProjectionSet
	value  *gomplate.Template
	when   *gomplate.Template
	match  *gomplate.Template
}

func compileProjection(projection Projection) (*compiledProjection, error) {
	var err error
	compiled := &compiledProjection{mappings: map[string]compiledProjectionSet{}}
	if projection.Spec.Source.Where != "" {
		compiled.where, err = compileProjectionExpression(projection.Spec.Source.Where)
		if err != nil {
			return nil, fmt.Errorf("spec.source.where: %w", err)
		}
	}
	if projection.Spec.Target != nil && projection.Spec.Target.Filter != "" {
		compiled.filter, err = compileProjectionExpression(projection.Spec.Target.Filter)
		if err != nil {
			return nil, fmt.Errorf("spec.target.filter: %w", err)
		}
	}
	for index, expression := range projection.Spec.Match {
		program, err := compileProjectionExpression(expression)
		if err != nil {
			return nil, fmt.Errorf("spec.match[%d]: %w", index, err)
		}
		compiled.matches = append(compiled.matches, program)
	}
	for path, mapping := range projection.Spec.Set {
		if _, err := jp.ParseString(path); err != nil {
			return nil, fmt.Errorf("spec.set key %q: %w", path, err)
		}
		value, err := compileProjectionExpression(mapping.Value)
		if err != nil {
			return nil, fmt.Errorf("spec.set[%q].value: %w", path, err)
		}
		entry := compiledProjectionSet{config: mapping, value: value}
		if mapping.When != "" {
			entry.when, err = compileProjectionExpression(mapping.When)
			if err != nil {
				return nil, fmt.Errorf("spec.set[%q].when: %w", path, err)
			}
		}
		if mapping.Match != "" {
			entry.match, err = compileProjectionExpression(mapping.Match)
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

	selected, filtered, err := filterProjectionSources(compiled, source)
	if err != nil {
		return result, fmt.Errorf("projection %s: %w", projection.Metadata.Name, err)
	}
	result.Filtered = filtered

	claimed := map[int][]string{}
	for _, sourceItem := range selected {
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
			claimed[len(targets)-1] = []string{identity}
			result.Created = append(result.Created, identity)
			continue
		}
		if len(matches) > 1 {
			return result, fmt.Errorf("projection %s source %s matches target indexes %v", projection.Metadata.Name, identity, matches)
		}
		index := matches[0]
		previous := claimed[index]
		if len(previous) > 0 {
			if !projection.Spec.Target.Aggregate {
				return result, fmt.Errorf("projection %s target index %d is matched by both %s and %s", projection.Metadata.Name, index, previous[0], identity)
			}
			result.Aggregated++
		} else {
			result.Matched++
		}
		claimed[index] = append(claimed[index], identity)
		changes, err := applyProjectionMappings(projectionMappingApplyOptions{
			Compiled:       compiled,
			File:           file,
			Selector:       projection.Spec.Target.Select,
			Index:          index,
			Source:         sourceItem,
			Target:         targets[index],
			Context:        source.Context,
			AggregatedWith: previous,
		})
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
	file, err := parser.ParseBytes(body, parser.ParseComments)
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

// filterProjectionSources drops source items rejected by spec.source.where and
// reports how many were dropped, so an over-eager predicate is visible in the
// result rather than indistinguishable from a clean run.
func filterProjectionSources(compiled *compiledProjection, source projectionSourceResult) ([]map[string]any, int, error) {
	if compiled.where == nil {
		return source.Items, 0, nil
	}
	selected := make([]map[string]any, 0, len(source.Items))
	filtered := 0
	for _, item := range source.Items {
		keep, err := evalProjectionBool(compiled.where, projectionActivation(item, map[string]any{}, source.Context, nil))
		if err != nil {
			return nil, 0, fmt.Errorf("source %s where: %w", projectionItemIdentity(item), err)
		}
		if !keep {
			filtered++
			continue
		}
		selected = append(selected, item)
	}
	return selected, filtered, nil
}

func selectProjectionSources(projection Projection, source projectionSourceResult) ([]map[string]any, int, error) {
	compiled, err := compileProjection(projection)
	if err != nil {
		return nil, 0, fmt.Errorf("projection %s: %w", projection.Metadata.Name, err)
	}
	return filterProjectionSources(compiled, source)
}

type projectionMappingApplyOptions struct {
	Compiled       *compiledProjection
	File           *ast.File
	Selector       string
	Index          int
	Source         map[string]any
	Target         map[string]any
	Context        map[string]any
	AggregatedWith []string
}

func applyProjectionMappings(options projectionMappingApplyOptions) ([]string, error) {
	var changed []string
	evaluationTarget, err := projectionMap(options.Target)
	if err != nil {
		return nil, fmt.Errorf("copy target for expression evaluation: %w", err)
	}
	for _, path := range projectionMappingPaths(options.Compiled) {
		mapping := options.Compiled.mappings[path]
		activation := projectionActivation(options.Source, evaluationTarget, options.Context, nil)
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
		current := jsonPath.Get(options.Target)
		if len(current) > 1 {
			return nil, fmt.Errorf("set %s selects %d values; each key must select at most one", path, len(current))
		}
		var currentValue any
		if len(current) == 1 {
			currentValue = current[0]
		}
		if len(options.AggregatedWith) > 0 && (mapping.config.Strategy == "" || mapping.config.Strategy == "replace") && !projectionValuesEqual(currentValue, value) {
			return nil, fmt.Errorf("aggregate scalar %s conflicts between %s and %s", path, options.AggregatedWith[0], projectionItemIdentity(options.Source))
		}
		value, err = mergeProjectionValue(mapping, currentValue, value, activation)
		if err != nil {
			return nil, fmt.Errorf("set %s: %w", path, err)
		}
		if projectionValuesEqual(currentValue, value) {
			continue
		}
		if err := jsonPath.SetOne(options.Target, value); err != nil {
			return nil, err
		}
		entryPath := strings.TrimSuffix(options.Selector, "[*]") + fmt.Sprintf("[%d]", options.Index)
		flowStyle, err := projectionTargetYAMLFlowStyle(options.File, entryPath)
		if err != nil {
			return nil, err
		}
		if len(current) == 0 {
			fragment := map[string]any{}
			if err := jsonPath.SetOne(fragment, value); err != nil {
				return nil, err
			}
			if err := mergeProjectionYAML(options.File, entryPath, fragment, flowStyle); err != nil {
				return nil, err
			}
		} else {
			// Replacing an existing value follows that value's own style, not the
			// entry's. A flow list inside a block-style entry re-rendered as a block
			// list gets indented to the column its inline value started at.
			valuePath := entryPath + strings.TrimPrefix(path, "$")
			valueFlowStyle, ok, err := projectionValueYAMLFlowStyle(options.File, valuePath)
			if err != nil {
				return nil, err
			}
			if ok {
				flowStyle = valueFlowStyle
			}
			if err := replaceProjectionYAML(options.File, valuePath, value, flowStyle); err != nil {
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

// projectionValueYAMLFlowStyle reports the flow style of the collection already at
// path. The second result is false when the path holds a scalar or is absent, in which
// case the caller keeps the enclosing entry's style.
func projectionValueYAMLFlowStyle(file *ast.File, path string) (bool, bool, error) {
	yamlPath, err := yaml.PathString(path)
	if err != nil {
		return false, false, projectionYAMLError(err)
	}
	node, err := yamlPath.FilterFile(file)
	if err != nil {
		return false, false, nil
	}
	switch typed := node.(type) {
	case *ast.SequenceNode:
		return typed.IsFlowStyle, true, nil
	case *ast.MappingNode:
		return typed.IsFlowStyle, true, nil
	default:
		return false, false, nil
	}
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

func replaceMatchingProjectionValues(program *gomplate.Template, current, incoming any, activation map[string]any) ([]any, error) {
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
