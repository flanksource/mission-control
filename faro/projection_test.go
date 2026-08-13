package main

import (
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

		projections, err := loadProjections(manifest)

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
			{Metadata: ProjectionMetadata{Name: "access"}},
			{Metadata: ProjectionMetadata{Name: "access"}},
		}

		Expect(validateProjectionNames(projections)).To(MatchError(`duplicate projection metadata.name "access"`))
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
