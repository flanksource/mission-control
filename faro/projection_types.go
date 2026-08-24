package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/duty/connection"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
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
	// Where is a CEL predicate over source and context evaluated per source item
	// before matching. Items that evaluate false are dropped and counted in
	// ProjectionApplyResult.Filtered.
	Where string `json:"where,omitempty" yaml:"where,omitempty"`
}

type ProjectionQuery struct {
	Configs        *ProjectionConfigsQuery        `json:"configs,omitempty" yaml:"configs,omitempty"`
	IdentityAccess *ProjectionIdentityAccessQuery `json:"identityAccess,omitempty" yaml:"identityAccess,omitempty"`
	Changes        *ProjectionChangesQuery        `json:"changes,omitempty" yaml:"changes,omitempty"`
	Insights       *ProjectionInsightsQuery       `json:"insights,omitempty" yaml:"insights,omitempty"`
	HTTP           *ProjectionHTTPQuery           `json:"http,omitempty" yaml:"http,omitempty"`
}

// ProjectionHTTPQuery reads rows from an HTTP API instead of the Mission Control
// catalog, for facts no scraper records.
//
// It is the one source kind that does not produce rows of its own. The APIs worth
// projecting are addressed per resource — GitHub has no "languages for every
// repository" endpoint, only /repos/{owner}/{repo}/languages — so the query reads
// the entries spec.target.select already picks out and issues one request per
// entry, with that entry bound to the URL template. Each response becomes that
// entry's source row, which makes spec.match structural rather than a lookup.
//
// The request is declared with duty's HTTPConnection rather than fields of our
// own: it is the shape the rest of the ecosystem already uses for "an HTTP
// endpoint in YAML", it keeps credentials in EnvVars instead of literals in a
// register repository, and it is already marked template:"true".
type ProjectionHTTPQuery struct {
	connection.HTTPConnection `json:",inline" yaml:",inline"`

	// Method defaults to GET.
	Method string `json:"method,omitempty" yaml:"method,omitempty"`

	// Select is a JSONPath into the response body, for payloads that nest the
	// object worth projecting. Omit it to use the whole body.
	Select string `json:"select,omitempty" yaml:"select,omitempty"`

	// Accepted bounds the wait when an API answers 202 while it computes the
	// answer. GitHub's statistics endpoints do this on a cold cache. Exhausting
	// the wait yields no row for that entry — never a zero, which would state
	// that a repository had no commits.
	Accepted ProjectionHTTPAccepted `json:"accepted,omitempty" yaml:"accepted,omitempty"`
}

type ProjectionHTTPAccepted struct {
	Attempts int           `json:"attempts,omitempty" yaml:"attempts,omitempty"`
	Wait     time.Duration `json:"wait,omitempty" yaml:"wait,omitempty"`
}

func (q ProjectionHTTPQuery) validate() error {
	if strings.TrimSpace(q.URL) == "" {
		return fmt.Errorf("spec.source.query.http.url is required")
	}
	if _, err := parseProjectionURLTemplate(q.URL); err != nil {
		return fmt.Errorf("spec.source.query.http.url: %w", err)
	}
	if q.Accepted.Attempts < 0 {
		return fmt.Errorf("spec.source.query.http.accepted.attempts must not be negative")
	}

	// HTTPConnection can express more than faro can honour. Every one of these
	// needs the database- and Kubernetes-backed context that hydrates connections
	// elsewhere, which faro does not have. Refusing them is the point: a
	// projection that declared oauth and silently sent an unauthenticated request
	// would fill a register with absent facts and look like a clean run.
	unsupported := map[string]bool{
		"connection": strings.TrimSpace(q.ConnectionName) != "",
		"oauth":      !q.OAuth.IsEmpty(),
		"awsSigV4":   q.AWSSigV4 != nil,
		"tls.ca":     !q.TLS.CA.IsEmpty(),
		"tls.cert":   !q.TLS.Cert.IsEmpty(),
		"tls.key":    !q.TLS.Key.IsEmpty(),
	}
	for _, field := range []string{"connection", "oauth", "awsSigV4", "tls.ca", "tls.cert", "tls.key"} {
		if unsupported[field] {
			return fmt.Errorf(
				"spec.source.query.http.%s is not supported: faro resolves credentials from the environment only, with no connection or secret store",
				field,
			)
		}
	}
	return nil
}

type ProjectionInsightsQuery struct {
	Search string `json:"search,omitempty" yaml:"search,omitempty"`
	Agent  string `json:"agent,omitempty" yaml:"agent,omitempty"`
	// Limit caps the whole result set, not one request; the query pages past the server's
	// per-request maximum. Omit it to project every matching insight.
	Limit int `json:"limit,omitempty" yaml:"limit,omitempty"`
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
	Limit          int                      `json:"limit" yaml:"limit"`
	PrincipalTypes []string                 `json:"principalTypes" yaml:"principalTypes"`
	UserTypes      []ProjectionUserTypeRule `json:"userTypes,omitempty" yaml:"userTypes,omitempty"`
}

func containsPrincipalType(principalTypes []string, expected string) bool {
	for _, principalType := range principalTypes {
		if principalType == expected {
			return true
		}
	}
	return false
}

func validatePrincipalTypes(principalTypes []string) error {
	seen := map[string]bool{}
	for index, principalType := range principalTypes {
		if principalType != "users" && principalType != "groups" {
			return fmt.Errorf("principalTypes[%d] must be users or groups", index)
		}
		if seen[principalType] {
			return fmt.Errorf("principalTypes[%d] duplicates %q", index, principalType)
		}
		seen[principalType] = true
	}
	return nil
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
	// Aggregate allows many sources to fold into one target. Scalar mappings must
	// be guarded, and conflicting writes fail during application.
	Aggregate bool `json:"aggregate,omitempty" yaml:"aggregate,omitempty"`
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

// projectionManifest is one file to load, and how it was reached. A file the caller named
// must be a Projection; a file found by walking a directory need not be, because registers
// and other manifests routinely sit beside projections in the same tree.
type projectionManifest struct {
	path       string
	discovered bool
}

// projectionManifests expands each path into the manifest files it names. A file is
// taken as given; a directory contributes every .yaml and .yml file beneath it, in the
// lexical order WalkDir guarantees, so a directory of documents loads in a stable order.
// A repeated path is loaded once, and a path naming nothing is an error rather than an
// empty contribution — a mistyped directory must not look like a clean run.
func projectionManifests(paths []string) ([]projectionManifest, error) {
	var manifests []projectionManifest
	at := map[string]int{}
	add := func(path string, discovered bool) {
		if index, ok := at[path]; ok {
			// A path reached both ways is held to the stricter rule: naming it
			// explicitly is a claim that it is a Projection.
			manifests[index].discovered = manifests[index].discovered && discovered
			return
		}
		at[path] = len(manifests)
		manifests = append(manifests, projectionManifest{path: path, discovered: discovered})
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			add(filepath.Clean(path), false)
			continue
		}
		found := 0
		if err := filepath.WalkDir(path, func(entry string, dir fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if dir.IsDir() || !isYAML(entry) {
				return nil
			}
			found++
			add(filepath.Clean(entry), true)
			return nil
		}); err != nil {
			return nil, err
		}
		if found == 0 {
			return nil, fmt.Errorf("%s contains no YAML documents", path)
		}
	}
	return manifests, nil
}

func isYAML(path string) bool {
	extension := filepath.Ext(path)
	return extension == ".yaml" || extension == ".yml"
}

// loadProjectionPaths loads every Projection named by paths, each of which is a manifest
// file or a directory of them, keeping the documents in the order the paths were given.
func loadProjectionPaths(paths []string) ([]Projection, error) {
	manifests, err := projectionManifests(paths)
	if err != nil {
		return nil, err
	}
	var projections []Projection
	for _, manifest := range manifests {
		loaded, err := loadProjections(manifest.path, projectionLoadOptions{SkipForeign: manifest.discovered})
		if err != nil {
			return nil, err
		}
		projections = append(projections, loaded...)
	}
	// A directory of YAML that holds no Projection at all is still a mistyped path
	// rather than a clean run, so the emptiness is caught here instead of per file.
	if len(projections) == 0 {
		return nil, fmt.Errorf("no Projection documents in %s", strings.Join(paths, ", "))
	}
	return projections, nil
}

// projectionLoadOptions controls what a document that is not a Projection means.
type projectionLoadOptions struct {
	// SkipForeign drops documents that are not Projections instead of failing, and is
	// set only for files found by walking a directory. A register, a values file or a
	// kustomization sitting beside the projections is ordinary and must not fail the
	// run; a file the caller named by hand is a claim that it holds a Projection, and
	// anything else there means the wrong path was given.
	SkipForeign bool
}

func loadProjections(path string, options projectionLoadOptions) ([]Projection, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	file, err := parser.ParseBytes(body, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, projectionYAMLError(err))
	}

	var projections []Projection
	for index, document := range file.Docs {
		if isEmptyProjectionDocument(document) {
			continue
		}
		// Classification has to happen before the strict decode: DisallowUnknownField
		// rejects any document that is not a Projection, so a foreign one cannot be
		// recognised by the decode that is meant to skip it.
		if options.SkipForeign && !isProjectionDocument(document) {
			continue
		}
		var projection Projection
		if err := yaml.NodeToValue(document.Body, &projection, yaml.DisallowUnknownField()); err != nil {
			return nil, fmt.Errorf("%s document %d: %w", path, index+1, projectionYAMLError(err))
		}
		projection.manifest = path
		if err := projection.validate(); err != nil {
			return nil, fmt.Errorf("%s document %d: %w", path, index+1, err)
		}
		projections = append(projections, projection)
	}
	if len(projections) == 0 && !options.SkipForeign {
		return nil, fmt.Errorf("%s contains no Projection documents", path)
	}
	return projections, nil
}

// isEmptyProjectionDocument reports the empty document a trailing `---` leaves behind,
// which is neither a Projection nor an error.
func isEmptyProjectionDocument(document *ast.DocumentNode) bool {
	if document == nil || document.Body == nil {
		return true
	}
	if _, null := document.Body.(*ast.NullNode); null {
		return true
	}
	return strings.TrimSpace(document.Body.String()) == ""
}

// isProjectionDocument reads apiVersion and kind alone, so a document can be identified
// without the strict decode that would reject it for fields Projection does not declare.
func isProjectionDocument(document *ast.DocumentNode) bool {
	var probe struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
	}
	if err := yaml.NodeToValue(document.Body, &probe); err != nil {
		return false
	}
	return probe.APIVersion == projectionAPIVersion && probe.Kind == projectionKind
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
		if len(identityAccess.PrincipalTypes) == 0 {
			return fmt.Errorf("spec.source.query.identityAccess.principalTypes must contain at least one principal type")
		}
		if err := validatePrincipalTypes(identityAccess.PrincipalTypes); err != nil {
			return fmt.Errorf("spec.source.query.identityAccess.%w", err)
		}
		if containsPrincipalType(identityAccess.PrincipalTypes, "users") && len(identityAccess.UserTypes) == 0 {
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
	if p.Spec.Source.Query.Insights != nil {
		queryCount++
		if p.Spec.Source.Query.Insights.Limit < 0 {
			return fmt.Errorf("spec.source.query.insights.limit must not be negative; omit it to project every matching insight")
		}
	}
	if p.Spec.Source.Query.HTTP != nil {
		queryCount++
		if err := p.Spec.Source.Query.HTTP.validate(); err != nil {
			return err
		}
		// The rows are the target's entries, so there is nothing to fan out over
		// without one. Caught here rather than at apply time, where it would look
		// like an empty API response.
		if p.Spec.Target == nil {
			return fmt.Errorf("spec.source.query.http requires spec.target; it issues one request per selected target entry")
		}
	}
	if queryCount != 1 {
		return fmt.Errorf("spec.source.query must contain exactly one of configs, identityAccess, changes, insights, or http")
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
		if p.Spec.Target.Aggregate && (mapping.Strategy == "" || mapping.Strategy == "replace") && strings.TrimSpace(mapping.When) == "" {
			return fmt.Errorf("spec.set[%q] must use strategy mergeUnique or replaceMatching, or guard its scalar write with when, because spec.target.aggregate folds many sources into one target", path)
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
