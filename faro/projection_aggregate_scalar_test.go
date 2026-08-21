package main

import (
	"os"

	"github.com/goccy/go-yaml"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("aggregate scalar projection mappings", func() {
	ginkgo.It("keeps the first guarded scalar while aggregating source mappings", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", "entries:\n  - id: canonical\n    external_users: []\n")
		projection := aggregateScalarProjection(targetPath, `!("name" in target)`)

		Expect(projection.validate()).To(Succeed())
		result, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{
			{"id": "first", "canonical_id": "canonical", "name": "First source"},
			{"id": "second", "canonical_id": "canonical", "name": "Second source"},
		}}, false)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Aggregated).To(Equal(1))
		body, err := os.ReadFile(targetPath)
		Expect(err).ToNot(HaveOccurred())
		var document map[string]any
		Expect(yaml.Unmarshal(body, &document)).To(Succeed())
		Expect(document["entries"]).To(Equal([]any{map[string]any{
			"id":             "canonical",
			"name":           "First source",
			"external_users": []any{"first", "second"},
		}}))
	})

	ginkgo.It("rejects conflicting scalar writes while aggregating", func() {
		dir := ginkgo.GinkgoT().TempDir()
		targetPath := writeProjectionTestFile(dir, "target.yaml", "entries:\n  - id: canonical\n    external_users: []\n")
		projection := aggregateScalarProjection(targetPath, "true")

		_, err := applyProjection(projection, projectionSourceResult{Items: []map[string]any{
			{"id": "first", "canonical_id": "canonical", "name": "First source"},
			{"id": "second", "canonical_id": "canonical", "name": "Second source"},
		}}, false)

		Expect(err).To(MatchError(ContainSubstring(`aggregate scalar $.name conflicts between first and second`)))
	})
})

func aggregateScalarProjection(targetPath, scalarWhen string) Projection {
	return Projection{
		APIVersion: projectionAPIVersion,
		Kind:       projectionKind,
		Metadata:   ProjectionMetadata{Name: "aggregate-scalars"},
		Spec: ProjectionSpec{
			Source: ProjectionSource{Query: ProjectionQuery{Insights: &ProjectionInsightsQuery{Limit: 10}}},
			Target: &ProjectionTarget{Path: targetPath, Select: "$.entries[*]", Aggregate: true},
			Match:  []string{"target.id == source.canonical_id"},
			Set: map[string]ProjectionSet{
				"$.name": {
					Value: "source.name",
					When:  scalarWhen,
				},
				"$.external_users": {
					Value:    "[source.id]",
					Strategy: "mergeUnique",
				},
			},
		},
	}
}
