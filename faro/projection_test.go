package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/goccy/go-yaml"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func columnNames(columns []api.ColumnDef) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names
}

func writeProjectionTestFile(dir, name, body string) string {
	path := filepath.Join(dir, name)
	Expect(os.WriteFile(path, []byte(body), 0o600)).To(Succeed())
	return path
}

var _ = ginkgo.Describe("Projection documents", func() {
	ginkgo.It("loads multi-document YAML as an ordered list", func() {
		manifest := writeProjectionTestFile(ginkgo.GinkgoT().TempDir(), "projections.yaml", `apiVersion: faro.flanksource.com/v1alpha1
kind: Projection
metadata:
  name: configs
spec:
  source:
    query:
      configs:
        configTypes: [Azure::Group]
        limit: 100
---
apiVersion: faro.flanksource.com/v1alpha1
kind: Projection
metadata:
  name: changes
spec:
  source:
    query:
      changes:
        changeTypes: [PermissionAdded, PermissionRemoved]
        lookback: 24h
        limit: 500
`)

		projections, err := loadProjections(manifest, projectionLoadOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(projections).To(HaveLen(2))
		Expect([]string{projections[0].Metadata.Name, projections[1].Metadata.Name}).To(Equal([]string{"configs", "changes"}))
		Expect(projections[1].Spec.Target).To(BeNil())
	})

	ginkgo.It("rejects documents containing more than one source query", func() {
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "ambiguous"},
			Spec: ProjectionSpec{Source: ProjectionSource{Query: ProjectionQuery{
				Configs:        &ProjectionConfigsQuery{Limit: 10},
				IdentityAccess: &ProjectionIdentityAccessQuery{Limit: 10, UserTypes: defaultIdentityTypeRules()},
			}}},
		}

		Expect(projection.validate()).To(MatchError(ContainSubstring("exactly one")))
	})

	ginkgo.It("rejects duplicate metadata names across documents", func() {
		projections := []Projection{
			{Metadata: ProjectionMetadata{Name: "access"}, manifest: "a.yaml"},
			{Metadata: ProjectionMetadata{Name: "access"}, manifest: "b.yaml"},
		}

		Expect(validateProjectionNames(projections)).
			To(MatchError(`duplicate projection metadata.name "access" in a.yaml and b.yaml`))
	})

	ginkgo.Describe("manifest paths", func() {
		const document = `apiVersion: faro.flanksource.com/v1alpha1
kind: Projection
metadata:
  name: %s
spec:
  source:
    query:
      configs:
        configTypes: [Azure::Group]
        limit: 100
`

		// A register sitting in the same directory as the projections that write it is the
		// normal layout, so every directory case below also exercises skipping it.
		const register = `schema_version: 1
systems:
  - config_id: 11111111-1111-1111-1111-111111111111
    name: example-cluster
`

		writeTree := func() string {
			root := ginkgo.GinkgoT().TempDir()
			Expect(os.MkdirAll(filepath.Join(root, "projections", "nested"), 0o750)).To(Succeed())
			writeProjectionTestFile(filepath.Join(root, "projections"), "changes.yaml", fmt.Sprintf(document, "changes"))
			writeProjectionTestFile(filepath.Join(root, "projections"), "access.yml", fmt.Sprintf(document, "access"))
			writeProjectionTestFile(filepath.Join(root, "projections"), "register.yaml", register)
			writeProjectionTestFile(filepath.Join(root, "projections", "nested"), "assets.yaml", fmt.Sprintf(document, "assets"))
			writeProjectionTestFile(root, "notes.md", "not a projection")
			return root
		}

		names := func(projections []Projection) []string {
			collected := make([]string, 0, len(projections))
			for _, projection := range projections {
				collected = append(collected, projection.Metadata.Name)
			}
			return collected
		}

		ginkgo.It("expands a directory into its YAML documents, recursing in sorted order", func() {
			projections, err := loadProjectionPaths([]string{filepath.Join(writeTree(), "projections")})

			Expect(err).ToNot(HaveOccurred())
			Expect(names(projections)).To(Equal([]string{"access", "changes", "assets"}))
		})

		ginkgo.It("concatenates several paths, mixing files and directories", func() {
			root := writeTree()

			projections, err := loadProjectionPaths([]string{
				filepath.Join(root, "projections", "changes.yaml"),
				filepath.Join(root, "projections", "nested"),
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(names(projections)).To(Equal([]string{"changes", "assets"}))
		})

		// Each document resolves spec.target against its own file, so a projection loaded
		// through a directory must keep the manifest it came from, not the directory.
		ginkgo.It("records the containing file as each document's manifest", func() {
			root := writeTree()

			projections, err := loadProjectionPaths([]string{filepath.Join(root, "projections")})

			Expect(err).ToNot(HaveOccurred())
			Expect(projections[2].manifest).To(Equal(filepath.Join(root, "projections", "nested", "assets.yaml")))
			Expect(projections[2].resolvePath("../../registers/assets.yaml")).
				To(Equal(filepath.Join(root, "registers", "assets.yaml")))
		})

		ginkgo.It("loads a repeated path once", func() {
			manifest := filepath.Join(writeTree(), "projections", "changes.yaml")

			projections, err := loadProjectionPaths([]string{manifest, manifest})

			Expect(err).ToNot(HaveOccurred())
			Expect(names(projections)).To(Equal([]string{"changes"}))
		})

		ginkgo.It("reports a directory holding no YAML documents", func() {
			_, err := loadProjectionPaths([]string{ginkgo.GinkgoT().TempDir()})

			Expect(err).To(MatchError(ContainSubstring("contains no YAML documents")))
		})

		ginkgo.It("skips a non-Projection document found by walking a directory", func() {
			projections, err := loadProjectionPaths([]string{filepath.Join(writeTree(), "projections")})

			Expect(err).ToNot(HaveOccurred())
			Expect(names(projections)).To(Equal([]string{"access", "changes", "assets"}))
			for _, projection := range projections {
				Expect(projection.manifest).ToNot(HaveSuffix("register.yaml"))
			}
		})

		// Naming a file is a claim that it holds a Projection, so the same document that is
		// skipped above must fail here — otherwise a mistyped path looks like a clean run.
		ginkgo.It("rejects a non-Projection file the caller named", func() {
			_, err := loadProjectionPaths([]string{filepath.Join(writeTree(), "projections", "register.yaml")})

			Expect(err).To(MatchError(ContainSubstring("register.yaml")))
			Expect(err).To(MatchError(ContainSubstring("document 1")))
		})

		ginkgo.It("reports a directory whose YAML holds no Projection", func() {
			root := ginkgo.GinkgoT().TempDir()
			writeProjectionTestFile(root, "register.yaml", register)

			_, err := loadProjectionPaths([]string{root})

			Expect(err).To(MatchError(ContainSubstring("no Projection documents")))
		})

		// The repository's own fixture directory holds target.yaml beside trust-center.yaml,
		// which is the layout that failed before foreign documents were skipped.
		ginkgo.It("loads the repository's projection fixtures", func() {
			projections, err := loadProjectionPaths([]string{filepath.Join("..", "fixtures", "projection")})

			Expect(err).ToNot(HaveOccurred())
			Expect(names(projections)).To(ContainElement("catalog-systems"))
		})

		ginkgo.It("reports a missing path rather than skipping it", func() {
			_, err := loadProjectionPaths([]string{filepath.Join(writeTree(), "absent")})

			Expect(err).To(MatchError(ContainSubstring("absent")))
		})
	})

	ginkgo.Describe("applying many projections", func() {
		// A projection that rewrites $.name on the one entry its target already holds,
		// so a successful apply is observable as a file change rather than a count alone.
		renameProjection := func(name, targetPath string) Projection {
			return Projection{
				APIVersion: projectionAPIVersion,
				Kind:       projectionKind,
				Metadata:   ProjectionMetadata{Name: name},
				Spec: ProjectionSpec{
					Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
					Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
					Match:  []string{"source.id == target.id"},
					Set:    map[string]ProjectionSet{"$.name": {Value: "source.name"}},
				},
			}
		}

		renameTarget := func(dir, name string) string {
			return writeProjectionTestFile(dir, name, "entries:\n  - {id: a, name: before}\n")
		}

		renameSource := func(Projection) (projectionSourceResult, error) {
			return projectionSourceResult{Items: []map[string]any{{"id": "a", "name": "after"}}}, nil
		}

		statuses := func(results []ProjectionApplyResult) []projectionStatus {
			collected := make([]projectionStatus, 0, len(results))
			for _, result := range results {
				collected = append(collected, result.Status)
			}
			return collected
		}

		// The regression this whole seam exists for: apply used to return on the first
		// error, so a mid-list failure discarded the results of everything before it and
		// never ran anything after it.
		ginkgo.It("runs the projections after a failing one, and writes their targets", func() {
			dir := ginkgo.GinkgoT().TempDir()
			projections := []Projection{
				renameProjection("first", renameTarget(dir, "first.yaml")),
				renameProjection("broken", renameTarget(dir, "broken.yaml")),
				renameProjection("last", renameTarget(dir, "last.yaml")),
			}
			query := func(projection Projection) (projectionSourceResult, error) {
				if projection.Metadata.Name == "broken" {
					return projectionSourceResult{}, fmt.Errorf("access query returned 2000 of 2079 rows")
				}
				return renameSource(projection)
			}

			results := applyProjections(projections, query, false)

			Expect(statuses(results)).To(Equal([]projectionStatus{projectionApplied, projectionFailed, projectionApplied}))
			Expect(results[1].Error).To(Equal("access query returned 2000 of 2079 rows"))
			for _, name := range []string{"first.yaml", "last.yaml"} {
				Expect(os.ReadFile(filepath.Join(dir, name))).To(Equal([]byte("entries:\n  - {id: a, name: after}\n")), name)
			}
			Expect(os.ReadFile(filepath.Join(dir, "broken.yaml"))).To(Equal([]byte("entries:\n  - {id: a, name: before}\n")))
		})

		// applyProjection returns a partially populated result alongside its error, and
		// those counts are the only evidence of how far the projection got.
		ginkgo.It("keeps the counts a failing projection had already established", func() {
			dir := ginkgo.GinkgoT().TempDir()
			projection := renameProjection("ambiguous", writeProjectionTestFile(dir, "target.yaml",
				"entries:\n  - {id: a, name: before}\n  - {id: a, name: before}\n"))

			results := applyProjections([]Projection{projection}, renameSource, false)

			Expect(results).To(HaveLen(1))
			Expect(results[0].Status).To(Equal(projectionFailed))
			Expect(results[0].Error).To(ContainSubstring("matches target indexes"))
			Expect(results[0].Sources).To(Equal(1))
		})

		ginkgo.It("reports a target-less projection as skipped without querying it", func() {
			dir := ginkgo.GinkgoT().TempDir()
			queried := []string{}
			query := func(projection Projection) (projectionSourceResult, error) {
				queried = append(queried, projection.Metadata.Name)
				return renameSource(projection)
			}
			reporting := Projection{
				APIVersion: projectionAPIVersion,
				Kind:       projectionKind,
				Metadata:   ProjectionMetadata{Name: "reporting"},
				Spec: ProjectionSpec{
					Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				},
			}

			results := applyProjections([]Projection{reporting, renameProjection("writes", renameTarget(dir, "target.yaml"))}, query, false)

			Expect(statuses(results)).To(Equal([]projectionStatus{projectionSkipped, projectionApplied}))
			Expect(queried).To(Equal([]string{"writes"}))
		})

		ginkgo.It("surfaces source warnings without failing the projection", func() {
			dir := ginkgo.GinkgoT().TempDir()
			query := func(Projection) (projectionSourceResult, error) {
				return projectionSourceResult{
					Items:    []map[string]any{{"id": "a", "name": "after"}},
					Warnings: []ProjectionWarning{{Source: "external-user-1", Message: "no namespace", Count: 2}},
				}, nil
			}

			results := applyProjections([]Projection{renameProjection("warns", renameTarget(dir, "target.yaml"))}, query, false)

			Expect(results[0].Status).To(Equal(projectionWarned))
			Expect(results[0].Warnings).To(HaveLen(1))
			Expect(results[0].Changed).ToNot(BeEmpty())
		})

		ginkgo.It("counts only failures towards the exit status", func() {
			Expect(failedProjections([]ProjectionApplyResult{
				{Status: projectionApplied}, {Status: projectionWarned}, {Status: projectionSkipped},
			})).To(Equal(0))
			Expect(failedProjections([]ProjectionApplyResult{
				{Status: projectionFailed}, {Status: projectionApplied}, {Status: projectionFailed},
			})).To(Equal(2))
		})

		// Each status must render as its own icon and colour; a shared rendering would
		// make a failed projection indistinguishable from a skipped one at a glance.
		ginkgo.It("renders every status distinctly in the summary table", func() {
			distinct := map[string]projectionStatus{}
			for _, status := range []projectionStatus{projectionApplied, projectionWarned, projectionSkipped, projectionFailed} {
				distinct[status.Pretty().ANSI()] = status
			}

			Expect(distinct).To(HaveLen(4))
		})

		// A jsonschema violation runs to dozens of lines. It is reported under the table
		// rather than inside a cell, and only where prose is welcome.
		ginkgo.It("reports each failure reason below the table, for pretty output only", func() {
			results := []ProjectionApplyResult{
				{Projection: "cloud-assets", Status: projectionFailed, Error: "target after apply:\nmissing property 'controls'"},
				{Projection: "data-stores", Status: projectionApplied},
			}

			report := projectionFailureReport(results)

			Expect(report).ToNot(BeNil())
			Expect(report.String()).To(ContainSubstring("cloud-assets"))
			Expect(report.String()).To(ContainSubstring("  missing property 'controls'"))
			Expect(report.String()).ToNot(ContainSubstring("data-stores"))
			Expect(projectionFailureReport(results[1:])).To(BeNil())

			// A whole-register schema violation reports every offending entry; echoing all
			// of it buries the summary that follows.
			long := strings.Repeat("at '/entries/0': value must be one of\n", 40)
			capped := projectionFailureReport([]ProjectionApplyResult{
				{Projection: "cloud-assets", Status: projectionFailed, Error: long},
			}).String()
			Expect(strings.Count(capped, "value must be one of")).To(Equal(projectionErrorLines))
			Expect(capped).To(ContainSubstring(fmt.Sprintf("… %d more lines", 40-projectionErrorLines)))

			Expect(rendersPretty(clicky.FormatOptions{})).To(BeTrue())
			Expect(rendersPretty(clicky.FormatOptions{Format: "pretty"})).To(BeTrue())
			Expect(rendersPretty(clicky.FormatOptions{JSON: true})).To(BeFalse())
			Expect(rendersPretty(clicky.FormatOptions{Format: "json"})).To(BeFalse())
		})

		ginkgo.It("exposes stable table columns for the summary", func() {
			result := ProjectionApplyResult{Projection: "cloud-assets", Status: projectionApplied, Matched: 3}

			Expect(columnNames(result.Columns())).To(ContainElements("projection", "status", "matched", "changed"))
			Expect(result.Row()["projection"]).To(Equal("cloud-assets"))
			Expect(result.Row()["matched"]).To(Equal(3))
			// Empty columns are dropped by the renderer, so a projection with no resolved
			// target must leave the cell blank rather than printing filepath.Base("") as ".".
			Expect(result.Row()["target"]).To(Equal(""))
		})

		// Pretty output states a failure reason twice already — in the report above the
		// table and in RowDetail — and a multi-line schema error in a cell collapses every
		// other column. CSV, markdown and HTML have neither route, so without the column
		// they render a bare `failed` that says nothing about why.
		ginkgo.It("adds the failure reason to the table only where nothing else carries it", func() {
			result := ProjectionApplyResult{
				Projection: "cloud-assets",
				Status:     projectionFailed,
				Error:      "target after apply:\nmissing property 'controls'",
			}
			restore := clicky.Flags.FormatOptions
			defer func() { clicky.Flags.FormatOptions = restore }()

			clicky.Flags.FormatOptions = clicky.FormatOptions{}
			Expect(columnNames(result.Columns())).ToNot(ContainElement("error"))
			Expect(result.Row()).ToNot(HaveKey("error"))

			clicky.Flags.FormatOptions = clicky.FormatOptions{CSV: true}
			Expect(columnNames(result.Columns())).To(ContainElement("error"))
			// Flattened, because a markdown or HTML cell cannot hold the newline.
			Expect(result.Row()["error"]).To(Equal("target after apply: missing property 'controls'"))
		})
	})

	ginkgo.It("requires exact counts before projecting bounded audit data", func() {
		Expect(requireCompleteProjection("changes", 10, -1, 100)).To(MatchError(ContainSubstring("did not report an exact total")))
		Expect(requireCompleteProjection("changes", 10, 11, 100)).To(MatchError(ContainSubstring("returned 10 of 11")))
		Expect(requireCompleteProjection("changes", 10, 10, 100)).To(Succeed())
	})

	ginkgo.It("maps typed config filters onto the native resource selector", func() {
		selector := configProjectionSelector(ProjectionConfigsQuery{
			Search:        "repository",
			ConfigTypes:   []string{"Github::Repository"},
			TagSelector:   "topic/mission-control=true",
			LabelSelector: "namespace=platform",
			FieldSelector: "Critical Alerts=0",
		})

		Expect(selector.Search).To(Equal("repository"))
		Expect([]string(selector.Types)).To(Equal([]string{"Github::Repository"}))
		Expect(selector.Agent).To(Equal("all"))
		Expect(selector.TagSelector).To(Equal("topic/mission-control=true"))
		Expect(selector.LabelSelector).To(Equal("namespace=platform"))
		Expect(selector.FieldSelector).To(Equal("Critical Alerts=0"))
	})

	ginkgo.It("renders dynamic projection values as text", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `text(source.health) + ":" + text(source.alerts) + ":" + text(source.disabled)`)
		Expect(err).ToNot(HaveOccurred())

		value, err := evalProjectionValue(program, map[string]any{
			"source": map[string]any{"health": "healthy", "alerts": float64(3), "disabled": true},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(value).To(Equal("healthy:3:true"))
	})

	ginkgo.It("preserves a CEL null as a native null", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `null`)
		Expect(err).ToNot(HaveOccurred())

		value, err := evalProjectionValue(program, map[string]any{})

		Expect(err).ToNot(HaveOccurred())
		Expect(value).To(BeNil())
	})

	ginkgo.It("applies JSONPath mappings and list strategies to one matched target", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", `schema_version: 1
entries:
  - external_id: group-1
    evidence: {}
    roles: [reader]
    grants:
      - context: beta
        role: old
      - context: prod
        role: owner
`)
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "groups"},
			manifest:   filepath.Join(dir, "projection.yaml"),
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"source.id == target.external_id"},
				Set: map[string]ProjectionSet{
					"$.evidence.config": {Value: "source.config"},
					"$.roles":           {Value: "source.roles", Strategy: "mergeUnique"},
					"$.grants":          {Value: "[source.grant]", Strategy: "replaceMatching", Match: "item.context == context.name"},
					"$.tenant":          {Value: "source.tenant", When: `source.tenant != ""`},
				},
			},
		}
		source := projectionSourceResult{
			Context: map[string]any{"name": "beta"},
			Items: []map[string]any{{
				"id":     "group-1",
				"config": map[string]any{"displayName": "Platform"},
				"roles":  []any{"reader", "owner"},
				"grant":  map[string]any{"context": "beta", "role": "reader"},
				"tenant": "",
			}},
		}

		result, err := applyProjection(projection, source, false)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Changed).To(ConsistOf("group-1 $.evidence.config", "group-1 $.roles", "group-1 $.grants"))
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		var document map[string]any
		Expect(yaml.Unmarshal(body, &document)).To(Succeed())
		entries := document["entries"].([]any)
		entry := entries[0].(map[string]any)
		Expect(entry["roles"]).To(Equal([]any{"reader", "owner"}))
		Expect(entry).NotTo(HaveKey("tenant"))
		Expect(entry["evidence"]).To(Equal(map[string]any{"config": map[string]any{"displayName": "Platform"}}))
		Expect(entry["grants"]).To(Equal([]any{
			map[string]any{"context": "beta", "role": "reader"},
			map[string]any{"context": "prod", "role": "owner"},
		}))
	})

	ginkgo.It("preserves flow-style target entries when replacing a list", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", "entries:\n  - {id: repo, evidence: [old], owner: kept}\n")
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "flow"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"source.id == target.id"},
				Set:    map[string]ProjectionSet{"$.evidence": {Value: "source.evidence"}},
			},
		}

		_, err := applyProjection(projection, projectionSourceResult{
			Items: []map[string]any{{"id": "repo", "evidence": []any{"new"}}},
		}, false)

		Expect(err).ToNot(HaveOccurred())
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(Equal("entries:\n  - {id: repo, evidence: [new], owner: kept}\n"))
	})

	ginkgo.It("creates unmatched targets by default", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", "entries: []\n")
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "create"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"source.id == target.id"},
				Set: map[string]ProjectionSet{
					"$.id":      {Value: "source.id"},
					"$.aliases": {Value: "source.aliases", Strategy: "mergeUnique"},
					"$.name":    {Value: "source.name"},
				},
			},
		}

		result, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{{
			"id": "principal-1", "name": "Principal", "aliases": []any{"principal@example.com"},
		}}}, false)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Created).To(Equal([]string{"principal-1"}))
		Expect(result.Missing).To(BeEmpty())
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		var document map[string]any
		Expect(yaml.Unmarshal(body, &document)).To(Succeed())
		Expect(document["entries"]).To(Equal([]any{map[string]any{
			"aliases": []any{"principal@example.com"}, "id": "principal-1", "name": "Principal",
		}}))
	})

	ginkgo.It("reports unmatched targets when creation is explicitly disabled", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", "entries: []\n")
		create := false
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "enrich"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]", Create: &create},
				Match:  []string{"source.id == target.id"},
				Set:    map[string]ProjectionSet{"$.name": {Value: "source.name"}},
			},
		}

		result, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{{
			"id": "principal-1", "name": "Principal",
		}}}, false)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Created).To(BeEmpty())
		Expect(result.Missing).To(Equal([]string{"principal-1"}))
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(Equal("entries: []\n"))
	})

	ginkgo.It("keeps block-style list indentation when appending to a matched target", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", `entries:
  - id: repo
    properties:
      - existing property
`)
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "indent"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"source.id == target.id"},
				Set: map[string]ProjectionSet{
					"$.properties": {Value: `["added property"]`, Strategy: "mergeUnique"},
				},
			},
		}

		_, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{{"id": "repo"}}}, false)

		Expect(err).ToNot(HaveOccurred())
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(Equal(`entries:
  - id: repo
    properties:
      - existing property
      - added property
`))
	})

	ginkgo.It("keeps indentation when a flow-style list inside a block-style entry grows", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", `entries:
  - id: repo
    properties: [not_evidenced]
    owner: kept
`)
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "mixed-style"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"source.id == target.id"},
				Set: map[string]ProjectionSet{
					"$.properties": {Value: `["one", "two"]`, Strategy: "mergeUnique"},
				},
			},
		}

		_, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{{"id": "repo"}}}, false)

		Expect(err).ToNot(HaveOccurred())
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
			Expect(len(line) - len(strings.TrimLeft(line, " "))).To(BeNumerically("<=", 6),
				"line is indented far past its parent key: %q", line)
		}
	})

	ginkgo.It("preserves leading comments such as the schema language-server link", func() {
		dir := ginkgo.GinkgoT().TempDir()
		header := "# yaml-language-server: $schema=./target.schema.json\n"
		targetPath := writeProjectionTestFile(dir, "target.yaml", header+`schema_version: 1
entries:
  - id: repo
    name: old
`)
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "comments"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"source.id == target.id"},
				Set:    map[string]ProjectionSet{"$.name": {Value: "source.name"}},
			},
		}

		_, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{{
			"id": "repo", "name": "new",
		}}}, false)

		Expect(err).ToNot(HaveOccurred())
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(HavePrefix(header))
		Expect(string(body)).To(ContainSubstring("name: new"))
	})

	ginkgo.It("renders timestamps as the calendar date registers record", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `date(source.first_observed)`)
		Expect(err).ToNot(HaveOccurred())

		value, err := evalProjectionValue(program, map[string]any{
			"source": map[string]any{"first_observed": "2026-02-06T13:30:38Z"},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(value).To(Equal("2026-02-06"))
	})

	ginkgo.It("capitalises vendor-lowercased names so registers read as proper nouns", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `title(source.distro) + " " + source.version`)
		Expect(err).ToNot(HaveOccurred())

		value, err := evalProjectionValue(program, map[string]any{
			"source": map[string]any{"distro": "ubuntu", "version": "24.04"},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(value).To(Equal("Ubuntu 24.04"))
	})

	// Config properties arrive as strings, so a register that wants to record a count or a
	// score as a number has no way to get one without this.
	ginkgo.It("parses a catalog property string into a number", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env,
			`{"high": number(source.properties["High Alerts"]), "score": number(source.properties["OpenSSF Score"])}`)
		Expect(err).ToNot(HaveOccurred())

		value, err := evalProjectionValue(program, map[string]any{
			"source": map[string]any{"properties": map[string]any{"High Alerts": "17", "OpenSSF Score": "6.9"}},
		})

		Expect(err).ToNot(HaveOccurred())
		// A count writes as 17, not 17.0 — every CEL result is routed through structpb,
		// whose only numeric type is a float64, so integral values are narrowed back.
		Expect(value).To(Equal(map[string]any{"high": int64(17), "score": 6.9}))
	})

	ginkgo.It("refuses a property that is not a number rather than reading it as zero", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `number(source.alerts)`)
		Expect(err).ToNot(HaveOccurred())

		_, err = evalProjectionValue(program, map[string]any{"source": map[string]any{"alerts": "several"}})

		Expect(err).To(MatchError(ContainSubstring(`cannot parse "several"`)))
	})

	// Converting a float64 beyond int64's range is undefined in Go and lands as the minimum
	// int64 on amd64, so a byte count past 2^63 would be written to a register as a large
	// negative number. It stays a double instead.
	ginkgo.It("keeps a number too large for an int as a double rather than overflowing it", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `number(source.bytes)`)
		Expect(err).ToNot(HaveOccurred())

		const beyondInt64 = "18446744073709551616" // 2^64
		value, err := evalProjectionValue(program, map[string]any{
			"source": map[string]any{"bytes": beyondInt64},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(value).To(BeNumerically(">", 0))
		Expect(value).To(Equal(math.Pow(2, 64)))
	})

	ginkgo.It("refuses NaN rather than writing it to a register", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `number(source.score)`)
		Expect(err).ToNot(HaveOccurred())

		_, err = evalProjectionValue(program, map[string]any{"source": map[string]any{"score": "NaN"}})

		Expect(err).To(MatchError(ContainSubstring("NaN")))
	})

	ginkgo.It("rejects a date() argument that is not a timestamp", func() {
		env, err := newProjectionEnv()
		Expect(err).ToNot(HaveOccurred())
		program, err := compileProjectionExpression(env, `date(source.first_observed)`)
		Expect(err).ToNot(HaveOccurred())

		_, err = evalProjectionValue(program, map[string]any{
			"source": map[string]any{"first_observed": "last tuesday"},
		})

		Expect(err).To(MatchError(ContainSubstring("RFC3339")))
	})

	ginkgo.It("requires exactly one source query including insights", func() {
		insights := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "insights"},
			Spec: ProjectionSpec{Source: ProjectionSource{Query: ProjectionQuery{
				Insights: &ProjectionInsightsQuery{Search: "status=open", Limit: 1000},
			}}},
		}
		Expect(insights.validate()).To(Succeed())

		// An omitted limit is how a projection asks for every matching insight; the query
		// pages past the server's per-request cap rather than truncating there.
		insights.Spec.Source.Query.Insights.Limit = 0
		Expect(insights.validate()).To(Succeed())

		insights.Spec.Source.Query.Insights.Limit = -1
		Expect(insights.validate()).To(MatchError(ContainSubstring("insights.limit must not be negative")))

		insights.Spec.Source.Query.Insights.Limit = 1000
		insights.Spec.Source.Query.Configs = &ProjectionConfigsQuery{Limit: 10}
		Expect(insights.validate()).To(MatchError(ContainSubstring("exactly one")))
	})

	ginkgo.It("drops source items rejected by spec.source.where and counts them", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", "entries: []\n")
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "overdue"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{
					Query: ProjectionQuery{Insights: &ProjectionInsightsQuery{Limit: 1000}},
					Where: `timestamp(source.first_observed) < timestamp(context.observed_at) - ` +
						`duration(source.severity == "critical" ? "168h" : "720h")`,
				},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"source.id == target.id"},
				Set:    map[string]ProjectionSet{"$.id": {Value: "source.id"}},
			},
		}
		source := projectionSourceResult{
			Context: map[string]any{"observed_at": "2026-08-10T00:00:00Z"},
			Items: []map[string]any{
				{"id": "critical-overdue", "severity": "critical", "first_observed": "2026-07-22T00:00:00Z"},
				{"id": "critical-inside", "severity": "critical", "first_observed": "2026-08-08T00:00:00Z"},
				{"id": "high-overdue", "severity": "high", "first_observed": "2023-05-01T00:00:00Z"},
				{"id": "high-inside", "severity": "high", "first_observed": "2026-08-01T00:00:00Z"},
			},
		}

		result, err := applyProjection(projection, source, false)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Filtered).To(Equal(2))
		Expect(result.Created).To(Equal([]string{"critical-overdue", "high-overdue"}))
	})

	ginkgo.It("folds many sources into one target when aggregation is enabled", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", `entries:
  - repository: flanksource/mission-control
    integrity_properties: [hand-authored property]
`)
		create := false
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "scorecard"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Insights: &ProjectionInsightsQuery{Limit: 1000}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]", Create: &create, Aggregate: true},
				Match:  []string{"target.repository == source.config.name"},
				Set: map[string]ProjectionSet{
					"$.integrity_properties": {
						Value:    `["OpenSSF " + source.analyzer + " score " + text(source.analysis.score)]`,
						Strategy: "replaceMatching",
						Match:    `item.startsWith("OpenSSF " + source.analyzer + " ")`,
					},
				},
			},
		}
		repo := map[string]any{"name": "flanksource/mission-control"}
		source := projectionSourceResult{Items: []map[string]any{
			{"id": "a", "analyzer": "Signed-Releases", "analysis": map[string]any{"score": float64(0)}, "config": repo},
			{"id": "b", "analyzer": "Token-Permissions", "analysis": map[string]any{"score": float64(9)}, "config": repo},
			{"id": "c", "analyzer": "Pinned-Dependencies", "analysis": map[string]any{"score": float64(5)}, "config": repo},
		}}

		result, err := applyProjection(projection, source, false)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Matched).To(Equal(1))
		Expect(result.Aggregated).To(Equal(2))
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		var document map[string]any
		Expect(yaml.Unmarshal(body, &document)).To(Succeed())
		entry := document["entries"].([]any)[0].(map[string]any)
		Expect(entry["integrity_properties"]).To(Equal([]any{
			"hand-authored property",
			"OpenSSF Signed-Releases score 0",
			"OpenSSF Token-Permissions score 9",
			"OpenSSF Pinned-Dependencies score 5",
		}))
	})

	ginkgo.It("still rejects two sources claiming one target when aggregation is off", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", "entries:\n  - repository: repo-1\n    checks: []\n")
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "collision"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Insights: &ProjectionInsightsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]"},
				Match:  []string{"target.repository == source.repository"},
				Set:    map[string]ProjectionSet{"$.checks": {Value: "[source.id]", Strategy: "mergeUnique"}},
			},
		}

		_, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{
			{"id": "first", "repository": "repo-1"},
			{"id": "second", "repository": "repo-1"},
		}}, false)

		Expect(err).To(MatchError(ContainSubstring("is matched by both first and second")))
	})

	ginkgo.It("rejects scalar mappings under aggregation because fan-in keeps only the last source", func() {
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "scalar-under-aggregate"},
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Insights: &ProjectionInsightsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: "target.yaml", Select: "$.entries[*]", Aggregate: true},
				Match:  []string{"target.repository == source.repository"},
				Set:    map[string]ProjectionSet{"$.deployment_approval": {Value: "source.reason"}},
			},
		}

		Expect(projection.validate()).To(MatchError(ContainSubstring("must use strategy mergeUnique or replaceMatching")))
	})

	ginkgo.It("validates the complete target document against its schema", func() {
		dir := ginkgo.GinkgoT().TempDir()
		writeProjectionTestFile(dir, "target.yaml", "schema_version: 1\nentries: []\n")
		writeProjectionTestFile(dir, "target.schema.json", `{
  "type": "object",
  "required": ["schema_version", "entries"],
  "properties": {
    "schema_version": {"const": 1},
    "entries": {"type": "array"}
  }
}`)
		projection := Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "schema"},
			manifest:   filepath.Join(dir, "projection.yaml"),
			Spec: ProjectionSpec{
				Source: ProjectionSource{Query: ProjectionQuery{Configs: &ProjectionConfigsQuery{Limit: 10}}},
				Target: &ProjectionTarget{Path: "target.yaml", Schema: "target.schema.json", Select: "$.entries[*]"},
				Match:  []string{"source.id == target.id"},
				Set:    map[string]ProjectionSet{"$.name": {Value: "source.name"}},
			},
		}

		Expect(verifyProjection(projection)).To(Succeed())
	})
})
