package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

const (
	projectionAPIVersion = "faro.flanksource.com/v1alpha1"
	projectionKind       = "Projection"
)

type Projection struct {
	APIVersion string             `json:"apiVersion" yaml:"apiVersion"`
	Kind       string             `json:"kind" yaml:"kind"`
	Metadata   ProjectionMetadata `json:"metadata" yaml:"metadata"`
	Spec       ProjectionSpec     `json:"spec" yaml:"spec"`
	manifest   string
}

type ProjectionMetadata struct {
	Name string `json:"name" yaml:"name"`
}

type ProjectionSpec struct {
	Source ProjectionSource         `json:"source" yaml:"source"`
	Target *ProjectionTarget        `json:"target,omitempty" yaml:"target,omitempty"`
	Match  []string                 `json:"match,omitempty" yaml:"match,omitempty"`
	Set    map[string]ProjectionSet `json:"set,omitempty" yaml:"set,omitempty"`
}

type ProjectionSource struct {
	Query ProjectionQuery `json:"query" yaml:"query"`
}

type ProjectionQuery struct {
	Configs        *ProjectionConfigsQuery        `json:"configs,omitempty" yaml:"configs,omitempty"`
	IdentityAccess *ProjectionIdentityAccessQuery `json:"identityAccess,omitempty" yaml:"identityAccess,omitempty"`
	Changes        *ProjectionChangesQuery        `json:"changes,omitempty" yaml:"changes,omitempty"`
}

type ProjectionConfigsQuery struct {
	Search        string   `json:"search,omitempty" yaml:"search,omitempty"`
	ConfigTypes   []string `json:"configTypes,omitempty" yaml:"configTypes,omitempty"`
	Agent         string   `json:"agent,omitempty" yaml:"agent,omitempty"`
	TagSelector   string   `json:"tagSelector,omitempty" yaml:"tagSelector,omitempty"`
	LabelSelector string   `json:"labelSelector,omitempty" yaml:"labelSelector,omitempty"`
	FieldSelector string   `json:"fieldSelector,omitempty" yaml:"fieldSelector,omitempty"`
	Limit         int      `json:"limit" yaml:"limit"`
}

type ProjectionIdentityAccessQuery struct {
	Limit     int                      `json:"limit" yaml:"limit"`
	UserTypes []ProjectionUserTypeRule `json:"userTypes" yaml:"userTypes"`
}

type ProjectionUserTypeRule struct {
	When         string `json:"when" yaml:"when"`
	IdentityType string `json:"identityType" yaml:"identityType"`
}

func (r ProjectionUserTypeRule) validate(index int) error {
	if strings.TrimSpace(r.When) == "" {
		return fmt.Errorf("userTypes[%d].when is required", index)
	}
	switch r.IdentityType {
	case "person", "workload_identity", "skip":
		return nil
	default:
		return fmt.Errorf("userTypes[%d].identityType must be person, workload_identity, or skip", index)
	}
}

type ProjectionChangesQuery struct {
	ChangeTypes []string `json:"changeTypes,omitempty" yaml:"changeTypes,omitempty"`
	Sources     []string `json:"sources,omitempty" yaml:"sources,omitempty"`
	Since       string   `json:"since,omitempty" yaml:"since,omitempty"`
	Lookback    string   `json:"lookback,omitempty" yaml:"lookback,omitempty"`
	Limit       int      `json:"limit" yaml:"limit"`
}

type ProjectionTarget struct {
	Path   string `json:"path" yaml:"path"`
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty"`
	Select string `json:"select" yaml:"select"`
	Filter string `json:"filter,omitempty" yaml:"filter,omitempty"`
	Create *bool  `json:"create,omitempty" yaml:"create,omitempty"`
}

func (t ProjectionTarget) CreatesTargets() bool {
	return t.Create == nil || *t.Create
}

type ProjectionWarning struct {
	Source  string `json:"source" yaml:"source"`
	Message string `json:"message" yaml:"message"`
	Count   int    `json:"count,omitempty" yaml:"count,omitempty"`
}

type ProjectionSet struct {
	Value    string `json:"value" yaml:"value"`
	When     string `json:"when,omitempty" yaml:"when,omitempty"`
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	Match    string `json:"match,omitempty" yaml:"match,omitempty"`
}

func loadProjections(path string) ([]Projection, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(body), yaml.DisallowUnknownField())
	var projections []Projection
	for document := 1; ; document++ {
		var projection Projection
		if err := decoder.Decode(&projection); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s document %d: %w", path, document, projectionYAMLError(err))
		}
		if projection.APIVersion == "" && projection.Kind == "" && projection.Metadata.Name == "" {
			continue
		}
		projection.manifest = path
		if err := projection.validate(); err != nil {
			return nil, fmt.Errorf("%s document %d: %w", path, document, err)
		}
		projections = append(projections, projection)
	}
	if len(projections) == 0 {
		return nil, fmt.Errorf("%s contains no Projection documents", path)
	}
	return projections, nil
}

func (p Projection) validate() error {
	if p.APIVersion != projectionAPIVersion {
		return fmt.Errorf("apiVersion must be %q", projectionAPIVersion)
	}
	if p.Kind != projectionKind {
		return fmt.Errorf("kind must be %q", projectionKind)
	}
	if strings.TrimSpace(p.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}

	queryCount := 0
	if p.Spec.Source.Query.Configs != nil {
		queryCount++
		if p.Spec.Source.Query.Configs.Limit <= 0 {
			return fmt.Errorf("spec.source.query.configs.limit must be greater than zero")
		}
	}
	if p.Spec.Source.Query.IdentityAccess != nil {
		queryCount++
		identityAccess := p.Spec.Source.Query.IdentityAccess
		if identityAccess.Limit <= 0 {
			return fmt.Errorf("spec.source.query.identityAccess.limit must be greater than zero")
		}
		if len(identityAccess.UserTypes) == 0 {
			return fmt.Errorf("spec.source.query.identityAccess.userTypes must contain at least one rule")
		}
		for index, rule := range identityAccess.UserTypes {
			if err := rule.validate(index); err != nil {
				return fmt.Errorf("spec.source.query.identityAccess.%w", err)
			}
		}
	}
	if p.Spec.Source.Query.Changes != nil {
		queryCount++
		if err := p.Spec.Source.Query.Changes.validate(); err != nil {
			return err
		}
	}
	if queryCount != 1 {
		return fmt.Errorf("spec.source.query must contain exactly one of configs, identityAccess, or changes")
	}

	if p.Spec.Target == nil {
		if len(p.Spec.Match) != 0 || len(p.Spec.Set) != 0 {
			return fmt.Errorf("spec.match and spec.set require spec.target")
		}
		return nil
	}
	if p.Spec.Target.Path == "" || p.Spec.Target.Select == "" {
		return fmt.Errorf("spec.target.path and spec.target.select are required")
	}
	if !strings.HasSuffix(p.Spec.Target.Select, "[*]") {
		return fmt.Errorf("spec.target.select must end in [*] so each selected target has an unambiguous write path")
	}
	if len(p.Spec.Match) == 0 {
		return fmt.Errorf("spec.match must contain at least one CEL expression")
	}
	if len(p.Spec.Set) == 0 {
		return fmt.Errorf("spec.set must contain at least one JSONPath key")
	}
	for path, mapping := range p.Spec.Set {
		if !strings.HasPrefix(path, "$.") {
			return fmt.Errorf("spec.set key %q must be a JSONPath relative to the selected target", path)
		}
		if strings.TrimSpace(mapping.Value) == "" {
			return fmt.Errorf("spec.set[%q].value is required", path)
		}
		switch mapping.Strategy {
		case "", "replace", "mergeUnique":
			if mapping.Match != "" {
				return fmt.Errorf("spec.set[%q].match is only valid with strategy replaceMatching", path)
			}
		case "replaceMatching":
			if mapping.Match == "" {
				return fmt.Errorf("spec.set[%q].match is required with strategy replaceMatching", path)
			}
		default:
			return fmt.Errorf("spec.set[%q].strategy %q is not supported", path, mapping.Strategy)
		}
	}
	return nil
}

func (q ProjectionChangesQuery) validate() error {
	if q.Limit <= 0 {
		return fmt.Errorf("spec.source.query.changes.limit must be greater than zero")
	}
	if q.Since != "" && q.Lookback != "" {
		return fmt.Errorf("spec.source.query.changes cannot set both since and lookback")
	}
	if q.Since != "" {
		if _, err := time.Parse(time.RFC3339, q.Since); err != nil {
			return fmt.Errorf("spec.source.query.changes.since must be RFC3339: %w", err)
		}
	}
	if q.Lookback != "" {
		if _, err := time.ParseDuration(q.Lookback); err != nil {
			return fmt.Errorf("spec.source.query.changes.lookback: %w", err)
		}
	}
	return nil
}

func (p Projection) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(p.manifest), path))
}

func selectProjections(projections []Projection, name string) ([]Projection, error) {
	if name == "" {
		return projections, nil
	}
	for _, projection := range projections {
		if projection.Metadata.Name == name {
			return []Projection{projection}, nil
		}
	}
	return nil, fmt.Errorf("projection %q not found", name)
}
